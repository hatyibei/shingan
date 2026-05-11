package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	baselineio "github.com/hatyibei/shingan/infrastructure/baseline"
	"github.com/hatyibei/shingan/infrastructure/factory"
	"github.com/hatyibei/shingan/infrastructure/parser"
	"github.com/hatyibei/shingan/plugin"
	"github.com/spf13/cobra"
)

// analyzeFlags holds the parsed flag values for the analyze subcommand.
type analyzeFlags struct {
	input         string
	format        string  // input format: "json" or "adk-go"
	output        string  // output format: "json" or "markdown"
	outputFile    string  // output file path (empty = stdout)
	minConfidence float64 // minimum confidence threshold (0.0 = include all)

	// Phase 2-E — progressive adoption & diff mode.
	since        string // --since=<git-ref>: analyze only files changed since this ref
	baseline     string // --baseline=<path>: suppress findings already in this baseline
	saveBaseline string // --save-baseline=<path>: write current findings as a new baseline

	// Phase 0.5 — operational trust.
	policy string // --policy=<path>: load .shingan.yaml severity policy; "" = auto-discover
}

// newAnalyzeCmd builds and returns the cobra.Command for "shingan analyze".
func newAnalyzeCmd() *cobra.Command {
	flags := &analyzeFlags{}

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a workflow graph for structural issues",
		Long: `analyze reads a WorkflowGraph from a file or directory, runs all built-in
analysis rules concurrently, and writes the findings to stdout (or a file).

Exit codes:
  0  No findings, or only Info-level findings
  1  At least one Warning finding (and no Critical findings)
  2  At least one Critical finding`,
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := executeAnalyze(flags)
			if err != nil {
				return err
			}
			os.Exit(code)
			return nil
		},
	}

	cmd.Flags().StringVar(&flags.input, "input", "", "Path to the workflow file or directory (required)")
	cmd.Flags().StringVar(&flags.format, "format", "json", "Input format: json, adk-go, samurai, langgraph, n8n, or crewai")
	cmd.Flags().StringVar(&flags.output, "output", "json", "Output format: json, markdown, or sarif")
	cmd.Flags().StringVar(&flags.outputFile, "output-file", "", "Output file path (default: stdout)")
	cmd.Flags().Float64Var(&flags.minConfidence, "min-confidence", 0.0, "Exclude findings with confidence below this threshold (0.0–1.0)")
	cmd.Flags().StringVar(&flags.since, "since", "", "Git ref (e.g. main, v0.4.0); analyze only files changed since this ref")
	cmd.Flags().StringVar(&flags.baseline, "baseline", "", "Path to baseline JSON; findings already present are suppressed")
	cmd.Flags().StringVar(&flags.saveBaseline, "save-baseline", "", "Path to write current findings as a new baseline JSON")
	cmd.Flags().StringVar(&flags.policy, "policy", "", "Path to .shingan.yaml policy file (severity overrides + per-path disable). Default: walk up from CWD looking for .shingan.yaml")

	_ = cmd.MarkFlagRequired("input")

	return cmd
}

// executeAnalyze contains all business logic for the analyze command.
// It returns the exit code (0/1/2) and any error encountered during execution.
func executeAnalyze(flags *analyzeFlags) (int, error) {
	// Apply defaults for zero-value flags (allows struct-literal construction in tests).
	inputFormat := flags.format
	if inputFormat == "" {
		inputFormat = "json"
	}
	outputFormat := flags.output
	if outputFormat == "" {
		outputFormat = "json"
	}

	// 1. Resolve --since FIRST so we can short-circuit before spawning any
	//    parser (in particular: --format=langgraph eagerly forks a Python
	//    worker, which must not happen for unchanged --since runs).
	//    Per code review (Codex P2-2): defer parser creation until we know
	//    we actually have work to do.
	var (
		changed  []string
		repoRoot string
	)
	if flags.since != "" {
		var err error
		changed, err = changedFiles(flags.since, flags.input)
		if err != nil {
			return 1, fmt.Errorf("resolve --since: %w", err)
		}
		// Resolve repo root once, so filterByChangedFiles can normalise
		// absolute Pos.File entries (e.g. LangGraph shim output) into
		// repo-relative coordinates for comparison.
		if root, err := gitRepoRoot(); err == nil {
			repoRoot = root
		}
		if len(changed) == 0 {
			// Nothing changed — emit an empty report through the normal
			// reporter path so CI still gets a valid artefact.
			// Parser was never created, so unchanged --since runs work
			// even on machines without python3/langgraph installed.
			//
			// Per Codex iter2 P2: if --save-baseline is also set, still
			// persist an empty baseline so progressive-adoption automation
			// can rely on the file existing on every run.
			if flags.saveBaseline != "" {
				if err := baselineio.Save(flags.saveBaseline, domain.NewBaselineFromFindings(nil)); err != nil {
					return 1, fmt.Errorf("save empty baseline: %w", err)
				}
			}
			return emitFindings(flags, outputFormat, nil)
		}
	}

	// 2. Create parser via ParserFactory (now that we know we have work).
	parserFactory := factory.NewParserFactory()
	workflowParser, err := parserFactory.Create(inputFormat)
	if err != nil {
		return 1, fmt.Errorf("create parser: %w", err)
	}

	// 3. Load graphs. Per ADR-012: directory inputs yield ONE graph per
	//    file (independent agent definitions are no longer merged into a
	//    single graph). Single-file inputs return a one-element slice.
	//    --since filtering still happens at the finding level (Codex P1).
	inputs, err := loadAsMulti(flags.input, inputFormat, workflowParser, nil)
	if err != nil {
		return 1, fmt.Errorf("load graph: %w", err)
	}

	// 4. Run all analysis rules per-graph; AnalyzeMulti stamps each
	//    finding with its originating SourceFile (ADR-012).
	//
	// Plugin rules (ADR-015, v0.9 Plugin SDK) are appended to the
	// built-in slice so the orchestrator sees them as first-class
	// rules. Plugins register themselves at `init()` time via
	// `plugin.MustRegister`, which means the set is fixed by what
	// the binary statically links — there's no runtime loader.
	analyzerFactory := factory.NewAnalyzerFactory()
	rules := append(analyzerFactory.CreateAll(), plugin.Rules()...)

	orchestrator := application.NewAnalysisOrchestrator()

	// Load policy: explicit --policy flag wins; otherwise walk up from
	// CWD looking for .shingan.yaml. Failures are non-fatal — analysis
	// proceeds with rule defaults if the policy file is malformed.
	policyPath := flags.policy
	if policyPath == "" {
		if discovered, _ := application.DiscoverPolicy(""); discovered != "" {
			policyPath = discovered
		}
	}
	if policyPath != "" {
		if loaded, err := application.LoadPolicy(policyPath); err == nil && loaded != nil {
			orchestrator.Policy = loaded
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load policy %q: %v\n", policyPath, err)
		}
	}

	findings := orchestrator.AnalyzeMulti(inputs, rules)

	// 4a. Apply --since at the FINDING level. With per-file graphs we now
	//     have two ways to attribute a finding to a file:
	//       - Finding.SourceFile (set by AnalyzeMulti from the per-file
	//         input path) — primary signal
	//       - Node.Pos.File (set by some parsers, e.g. ADK-Go ParseFile)
	//         — fallback for legacy callers that bypass AnalyzeMulti
	//     filterByChangedFilesMulti consults both, preferring SourceFile.
	if flags.since != "" {
		findings = filterByChangedFilesMulti(findings, inputs, changed, repoRoot)
	}

	// 4b. Filter by minimum confidence threshold if specified.
	if flags.minConfidence > 0.0 {
		findings = filterByConfidence(findings, flags.minConfidence)
	}

	// 4c. Apply baseline suppression BEFORE save-baseline so a combined
	//     --baseline + --save-baseline run saves only the newly-introduced
	//     findings (matches the phase2plan pseudocode exactly).
	if flags.baseline != "" {
		b, err := baselineio.Load(flags.baseline)
		if err != nil {
			return 1, fmt.Errorf("load baseline: %w", err)
		}
		findings = filterNew(findings, b)
	}

	// 4d. Persist the (possibly filtered) findings as a new baseline.
	if flags.saveBaseline != "" {
		if err := baselineio.Save(flags.saveBaseline, domain.NewBaselineFromFindings(findings)); err != nil {
			return 1, fmt.Errorf("save baseline: %w", err)
		}
	}

	return emitFindings(flags, outputFormat, findings)
}

// emitFindings renders findings with the configured reporter, writes them to
// stdout/file, and returns the exit code. Factored out so that the --since
// short-circuit and the normal path share identical report formatting and
// exit-code semantics.
func emitFindings(flags *analyzeFlags, outputFormat string, findings []domain.Finding) (int, error) {
	reporterFactory := factory.NewReporterFactory()
	formatter, err := reporterFactory.Create(outputFormat)
	if err != nil {
		return 1, fmt.Errorf("create reporter: %w", err)
	}

	output, err := formatter.Format(findings)
	if err != nil {
		return 1, fmt.Errorf("format findings: %w", err)
	}

	if err := writeOutput(flags.outputFile, output); err != nil {
		return 1, fmt.Errorf("write output: %w", err)
	}

	return exitCode(findings), nil
}

// loadGraphWithParser loads and parses a WorkflowGraph from path using the given parser.
// For adk-go format with a directory input, all *.go files are walked and their
// nodes and edges are merged into a single graph.
//
// Deprecated: as of ADR-012, directory inputs yield one graph per file.
// Use loadAsMulti for new callers; this wrapper exists only for legacy
// integrations that still need a single-graph view (and tolerate the
// merge-induced false positives the ADR catalogues).
func loadGraphWithParser(path, inputFormat string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
	return loadGraphFiltered(path, inputFormat, p, nil)
}

// loadAsMulti returns one (graph, sourceFile) pair per parsed file. Per
// ADR-012 this is the canonical loader for the analyze pipeline:
//
//	single file input  → one-element slice with that file's path
//	directory input    → one element per matching .go (adk-go) /
//	                     .py (langgraph) file, each parsed independently
//	                     so independent agent definitions don't merge
//
// `allow` is a repo-relative path allowlist used by --since: when
// non-nil, files outside the set are skipped entirely. Empty allowlist
// (allow != nil but len == 0) returns an empty slice.
func loadAsMulti(path, inputFormat string, p application.WorkflowParser, allow []string) ([]application.GraphWithSource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if !info.IsDir() {
		// Single-file input. Honour --since allowlist if present.
		if allow != nil && !fileInAllowlist(path, allow) {
			return []application.GraphWithSource{}, nil
		}
		g, err := parseFile(path, p)
		if err != nil {
			return nil, err
		}
		return []application.GraphWithSource{{Graph: g, SourceFile: path}}, nil
	}

	// Directory input — pick the extension by format.
	var ext string
	switch inputFormat {
	case "adk-go":
		ext = ".go"
	case "langgraph", "crewai":
		ext = ".py"
	default:
		return nil, fmt.Errorf("directory input is only supported for adk-go, langgraph, and crewai formats; use a single JSON file for json/samurai/n8n formats")
	}
	return parseSourceDirectoryAsMulti(path, p, allow, ext)
}

// parseSourceDirectoryAsMulti walks a directory, parses every matching
// file independently, and returns one GraphWithSource per file. Per
// ADR-012 — the previous parseSourceDirectoryFiltered merged everything
// into one graph and produced spurious unreachable_node findings on
// directories holding multiple independent agent definitions.
//
// Files that fail to parse are skipped with a warning (mirrors CLI
// resilience for incremental refactors); fatal walk errors propagate.
func parseSourceDirectoryAsMulti(dir string, p application.WorkflowParser, allow []string, ext string) ([]application.GraphWithSource, error) {
	var out []application.GraphWithSource
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk error at %q: %w", path, walkErr)
		}
		if info.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		if allow != nil && !fileInAllowlist(path, allow) {
			return nil
		}
		g, err := parseFile(path, p)
		if err != nil {
			// Codex iter4 P2: propagate framework-missing errors so CI
			// fails loudly when `pip install <fw>` is missing, instead
			// of silently producing 0 findings. Single-file syntax
			// errors continue to be skipped with a warning.
			if errors.Is(err, parser.ErrPythonFrameworkMissing) {
				return fmt.Errorf("crewai/langgraph framework not installed: %w", err)
			}
			_, _ = fmt.Fprintf(os.Stderr, "warning: skipping %q: %v\n", path, err)
			return nil
		}
		out = append(out, application.GraphWithSource{Graph: g, SourceFile: path})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk directory %q: %w", dir, walkErr)
	}
	return out, nil
}

// loadGraphFiltered is loadGraphWithParser with an optional allowlist of file
// paths. When allow is non-nil, only files whose paths match one of the
// entries are parsed — used by --since to restrict analysis to changed files.
// When allow is nil, all files are parsed (original behaviour).
func loadGraphFiltered(path, inputFormat string, p application.WorkflowParser, allow []string) (*domain.WorkflowGraph, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if !info.IsDir() {
		// Single file: honour allowlist if provided.
		if allow != nil && !fileInAllowlist(path, allow) {
			// File is out of scope — return an empty graph.
			return &domain.WorkflowGraph{Nodes: make(map[string]*domain.Node)}, nil
		}
		return parseFile(path, p)
	}

	// Directory input. Per format-specific extension.
	switch inputFormat {
	case "adk-go":
		return parseSourceDirectoryFiltered(path, p, allow, ".go")
	case "langgraph", "crewai":
		return parseSourceDirectoryFiltered(path, p, allow, ".py")
	default:
		return nil, fmt.Errorf("directory input is only supported for adk-go, langgraph, and crewai formats; use a single JSON file for json/samurai/n8n formats")
	}
}

// fileParser is an optional capability some parsers expose to receive a path
// (so they can resolve language-specific imports relative to the file's
// directory) instead of an opaque byte slice. Currently implemented by
// LangGraphParser (sys.path resolution) and ADKGoParser (go/types pass).
type fileParser interface {
	ParseFile(path string) (*domain.WorkflowGraph, error)
}

// parseFile reads a single file and parses it with the given parser.
// Parsers that implement `fileParser` receive the path directly so language-
// specific module resolution can succeed (e.g. LangGraph's sys.path lookup
// for sibling modules, ADK-Go's go/types second pass for generic instances).
func parseFile(path string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
	if fp, ok := p.(fileParser); ok {
		graph, err := fp.ParseFile(path)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", path, err)
		}
		return graph, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}
	graph, err := p.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return graph, nil
}

// parseADKGoDirectory walks a directory recursively, parses all *.go files,
// and merges their nodes and edges into a single WorkflowGraph.
// The entry node comes from the first file that defines one.
//
// Kept as a thin wrapper for backward compatibility — internal callers should
// use parseSourceDirectoryFiltered with the desired extension.
func parseADKGoDirectory(dir string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
	return parseSourceDirectoryFiltered(dir, p, nil, ".go")
}

// parseADKGoDirectoryFiltered is parseADKGoDirectory with an optional
// allowlist; when non-nil, only files present in allow are parsed.
func parseADKGoDirectoryFiltered(dir string, p application.WorkflowParser, allow []string) (*domain.WorkflowGraph, error) {
	return parseSourceDirectoryFiltered(dir, p, allow, ".go")
}

// parseSourceDirectoryFiltered walks a directory recursively, parses all
// files matching extension `ext`, and merges their nodes and edges into a
// single WorkflowGraph. Used by both adk-go (`.go`) and langgraph (`.py`)
// inputs to share the same merge / dedup / allowlist logic.
//
// `ext` must include the leading dot (e.g. ".go", ".py").
func parseSourceDirectoryFiltered(dir string, p application.WorkflowParser, allow []string, ext string) (*domain.WorkflowGraph, error) {
	merged := &domain.WorkflowGraph{
		Nodes: make(map[string]*domain.Node),
	}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk error at %q: %w", path, walkErr)
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ext {
			return nil
		}
		if allow != nil && !fileInAllowlist(path, allow) {
			return nil
		}

		g, err := parseFile(path, p)
		if err != nil {
			// Non-fatal: skip files that can't be parsed (e.g. syntax errors).
			// Wrap and return so callers can see the error chain; return nil to continue.
			_, _ = fmt.Fprintf(os.Stderr, "warning: skipping %q: %v\n", path, err)
			return nil
		}

		// Merge nodes.
		for id, node := range g.Nodes {
			if _, exists := merged.Nodes[id]; !exists {
				merged.Nodes[id] = node
			}
		}
		// Merge edges (deduplicate by From+To+Condition).
		for _, edge := range g.Edges {
			if !edgeExists(merged.Edges, edge) {
				merged.Edges = append(merged.Edges, edge)
			}
		}
		// Use first-encountered entry node.
		if merged.EntryNodeID == "" && g.EntryNodeID != "" {
			merged.EntryNodeID = g.EntryNodeID
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %q: %w", dir, err)
	}

	return merged, nil
}

// edgeExists returns true if edge is already in edges.
func edgeExists(edges []domain.Edge, e domain.Edge) bool {
	for _, existing := range edges {
		if existing.From == e.From && existing.To == e.To && existing.Condition == e.Condition {
			return true
		}
	}
	return false
}

// loadGraph reads and parses a WorkflowGraph from a JSON file.
// Kept for backward compatibility with existing tests.
func loadGraph(path string) (*domain.WorkflowGraph, error) {
	parserFactory := factory.NewParserFactory()
	p, err := parserFactory.Create("json")
	if err != nil {
		return nil, fmt.Errorf("create json parser: %w", err)
	}
	return parseFile(path, p)
}

// writeOutput writes data to a file if path is non-empty, otherwise to stdout.
func writeOutput(path string, data []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		if err != nil {
			return fmt.Errorf("write to stdout: %w", err)
		}
		// Add a trailing newline for terminal readability.
		_, _ = fmt.Fprintln(os.Stdout)
		return nil
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

// filterByConfidence returns only findings whose Confidence >= minConfidence.
func filterByConfidence(findings []domain.Finding, minConfidence float64) []domain.Finding {
	filtered := make([]domain.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Confidence >= minConfidence {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// exitCode calculates the appropriate CLI exit code for a set of findings.
//
//	0 — no findings, or only Info-level
//	1 — at least one Warning (and no Critical)
//	2 — at least one Critical
func exitCode(findings []domain.Finding) int {
	code := 0
	for _, f := range findings {
		switch f.Severity {
		case domain.Warning:
			if code < 1 {
				code = 1
			}
		case domain.Critical:
			return 2
		}
	}
	return code
}

// filterNew returns only findings whose fingerprint is NOT already present in
// the baseline. Used to suppress pre-existing issues during progressive adoption.
func filterNew(findings []domain.Finding, b *domain.Baseline) []domain.Finding {
	if b == nil {
		return findings
	}
	out := make([]domain.Finding, 0, len(findings))
	for _, f := range findings {
		if !b.Contains(f) {
			out = append(out, f)
		}
	}
	return out
}

// changedFiles runs `git diff --name-only <since>..HEAD` and returns paths
// (repo-root relative, filepath.Clean'd) that fall under inputPrefix.
//
// Per Codex P2-1 review: git always emits repo-root relative paths, but
// inputPrefix may be absolute, or the user may invoke shingan from a
// subdirectory of the repo. We resolve inputPrefix against `git rev-parse
// --show-toplevel` and then compare in repo-root coordinates so absolute
// and subdirectory invocations both match correctly.
//
// If git is unavailable or the diff fails, an error is returned: silently
// treating --since as a no-op would defeat the purpose of progressive adoption.
//
// since is rejected if it starts with "-" so that values like "--exec=evil"
// can never be smuggled in as a git CLI option (defense-in-depth: exec.Command
// already avoids shell interpretation, but git itself would still parse a
// leading "-" as an option flag).
func changedFiles(since, inputPrefix string) ([]string, error) {
	if strings.HasPrefix(since, "-") {
		return nil, fmt.Errorf("--since value must not start with '-': %q", since)
	}

	// Discover repo root so we can normalise inputPrefix to repo-relative form.
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("locate git repo root: %w", err)
	}

	prefix, err := repoRelativePrefix(inputPrefix, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("normalise --input %q against repo root: %w", inputPrefix, err)
	}

	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s..HEAD", since))
	cmd.Dir = repoRoot // run diff from repo root so paths come out repo-relative regardless of CWD
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s..HEAD: %w", since, err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return []string{}, nil
	}

	lines := strings.Split(raw, "\n")
	result := make([]string, 0, len(lines))
	for _, f := range lines {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		clean := filepath.Clean(f)
		// Exact match (single-file input) or descendant of a directory input.
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			result = append(result, clean)
		}
	}
	return result, nil
}

// fileInAllowlist reports whether path matches any entry in allow, using
// cleaned-path comparison. Both `path` (often absolute, from filepath.Walk)
// and allow entries (repo-root relative, from git diff) are compared by
// suffix to support cross-CWD invocations.
func fileInAllowlist(path string, allow []string) bool {
	clean := filepath.Clean(path)
	for _, a := range allow {
		if a == clean {
			return true
		}
		// Also match when path is absolute and allow entry is repo-relative:
		// if path ends with "/<allow>" we count it as a match. Avoids false
		// negatives when shingan is invoked with --input=/abs/path.
		sep := string(filepath.Separator)
		if strings.HasSuffix(clean, sep+a) {
			return true
		}
	}
	return false
}

// gitRepoRoot returns the absolute path of the current repository root, as
// reported by `git rev-parse --show-toplevel`. Used to normalise --input
// against repo-relative paths emitted by `git diff --name-only`.
func gitRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// repoRelativePrefix turns inputPrefix (which may be absolute, relative to
// CWD, or already relative to repoRoot) into a repo-root-relative cleaned
// path suitable for prefix-matching against `git diff --name-only` output.
//
// The returned prefix is filepath.Clean'd and uses forward-or-OS separators
// consistent with git's output. Returns an error if inputPrefix lies outside
// repoRoot.
func repoRelativePrefix(inputPrefix, repoRoot string) (string, error) {
	abs, err := filepath.Abs(inputPrefix)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("input %q is outside repo root %q", inputPrefix, repoRoot)
	}
	return filepath.Clean(rel), nil
}

// filterByChangedFilesMulti is the ADR-012 successor to
// filterByChangedFiles. With per-file independent graphs every Finding
// carries its originating file in SourceFile; we look up the changed
// set against that primary signal first, then fall back to Node.Pos.File
// (set by some parsers like ADK-Go ParseFile / LangGraph) and finally
// keep the finding defensively if neither attribution exists.
//
// `inputs` is the same []GraphWithSource passed to AnalyzeMulti so we
// can resolve a finding's NodeID → Node when only Node.Pos.File is set.
//
// repoRoot lets normaliseToRepoRelative collapse absolute parser paths
// (e.g. LangGraph shim) into the same coordinate system as `git diff
// --name-only` output (Codex iter2 P1 fix, generalised).
func filterByChangedFilesMulti(findings []domain.Finding, inputs []application.GraphWithSource, changed []string, repoRoot string) []domain.Finding {
	if len(changed) == 0 {
		return nil
	}
	changedSet := make(map[string]struct{}, len(changed))
	for _, c := range changed {
		changedSet[filepath.Clean(c)] = struct{}{}
	}

	// Index nodes by (sourceFile → nodeID → node) so SourceFile-less
	// findings can resort to Node.Pos.File.
	type nodeIndex map[string]*domain.Node
	bySrc := make(map[string]nodeIndex, len(inputs))
	for _, in := range inputs {
		if in.Graph == nil {
			continue
		}
		idx := make(nodeIndex, len(in.Graph.Nodes))
		for id, n := range in.Graph.Nodes {
			idx[id] = n
		}
		bySrc[in.SourceFile] = idx
	}

	out := make([]domain.Finding, 0, len(findings))
	for _, f := range findings {
		// Primary: Finding.SourceFile (ADR-012 attribution).
		if f.SourceFile != "" {
			key := normaliseToRepoRelative(f.SourceFile, repoRoot)
			if _, hit := changedSet[key]; hit {
				out = append(out, f)
			}
			continue
		}

		// Secondary: look up Node.Pos.File via per-source index.
		var node *domain.Node
		for _, idx := range bySrc {
			if n, ok := idx[f.NodeID]; ok && n != nil {
				node = n
				break
			}
		}
		if node == nil || node.Pos.IsZero() {
			// Defensive keep: better surface than silent drop.
			out = append(out, f)
			continue
		}
		key := normaliseToRepoRelative(node.Pos.File, repoRoot)
		if _, hit := changedSet[key]; hit {
			out = append(out, f)
		}
	}
	return out
}

// filterByChangedFiles keeps only findings whose associated node lives in a
// file in the changed set. Findings without source-position information
// (Node.Pos.IsZero() or node not found in graph) are kept defensively —
// better to surface a finding than to hide it silently.
//
// Per Codex P1 review: --since must operate at the finding level, not at
// the graph-construction level, so the analyzer can reason about the full
// workflow topology before suppressing pre-existing findings.
//
// Per Codex iter2 P1 review: changed paths are repo-relative (from
// `git diff --name-only` run at repo root), but Node.Pos.File can be
// absolute (LangGraph shim) or repo-relative (ADK-Go ParseFile). Both
// sides are normalised to repo-relative coordinates before comparison so
// the LangGraph + --since combination doesn't silently drop findings.
// repoRoot is the absolute repo-root path; if empty, paths are compared
// as-is (for unit tests and degraded environments).
func filterByChangedFiles(findings []domain.Finding, graph *domain.WorkflowGraph, changed []string, repoRoot string) []domain.Finding {
	if len(changed) == 0 {
		return nil
	}
	changedSet := make(map[string]struct{}, len(changed))
	for _, c := range changed {
		changedSet[filepath.Clean(c)] = struct{}{}
	}

	out := make([]domain.Finding, 0, len(findings))
	for _, f := range findings {
		node, ok := graph.Nodes[f.NodeID]
		if !ok || node == nil {
			// Finding doesn't reference a graph node — keep defensively.
			out = append(out, f)
			continue
		}
		if node.Pos.IsZero() {
			// No source position — keep defensively (single-file inputs and
			// older parsers don't always populate Pos).
			out = append(out, f)
			continue
		}
		key := normaliseToRepoRelative(node.Pos.File, repoRoot)
		if _, hit := changedSet[key]; hit {
			out = append(out, f)
		}
		// else: finding lives in an unchanged file — suppress.
	}
	return out
}

// normaliseToRepoRelative converts a source path to a repo-relative
// cleaned form for comparison against `git diff --name-only` output.
//
// Per Codex iter6 P2: relative paths (e.g. "file.go" passed via --input)
// must be resolved through the current working directory before being
// made repo-relative. A user invoking shingan from a subdirectory with
// `--input file.go` previously got SourceFile="file.go" while git diff
// emitted "pkg/file.go", causing --since to silently drop the finding.
// We now: (1) make the path absolute via filepath.Abs (resolves CWD),
// (2) make it relative to repoRoot. Falls back to the cleaned input
// when either step fails or the path is outside repoRoot.
//
// repoRoot may be empty (test mode) — in that case path is just cleaned
// without CWD or repo resolution.
func normaliseToRepoRelative(path, repoRoot string) string {
	clean := filepath.Clean(path)
	if repoRoot == "" {
		return clean
	}
	abs := clean
	if !filepath.IsAbs(abs) {
		resolved, err := filepath.Abs(clean)
		if err != nil {
			return clean
		}
		abs = resolved
	}
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return clean
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		// Outside the repo — leave as-is so it never matches the changed set.
		return clean
	}
	return filepath.Clean(rel)
}
