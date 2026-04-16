package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	baselineio "github.com/hatyibei/shingan/infrastructure/baseline"
	"github.com/hatyibei/shingan/infrastructure/factory"
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
	cmd.Flags().StringVar(&flags.format, "format", "json", "Input format: json or adk-go")
	cmd.Flags().StringVar(&flags.output, "output", "json", "Output format: json, markdown, or sarif")
	cmd.Flags().StringVar(&flags.outputFile, "output-file", "", "Output file path (default: stdout)")
	cmd.Flags().Float64Var(&flags.minConfidence, "min-confidence", 0.0, "Exclude findings with confidence below this threshold (0.0–1.0)")
	cmd.Flags().StringVar(&flags.since, "since", "", "Git ref (e.g. main, v0.4.0); analyze only files changed since this ref")
	cmd.Flags().StringVar(&flags.baseline, "baseline", "", "Path to baseline JSON; findings already present are suppressed")
	cmd.Flags().StringVar(&flags.saveBaseline, "save-baseline", "", "Path to write current findings as a new baseline JSON")

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

	// 1. Create parser via ParserFactory.
	parserFactory := factory.NewParserFactory()
	workflowParser, err := parserFactory.Create(inputFormat)
	if err != nil {
		return 1, fmt.Errorf("create parser: %w", err)
	}

	// 2. Resolve --since: if the current input has no changed files since <ref>,
	//    short-circuit to 0 findings (progressive adoption: unchanged code stays
	//    silent). Otherwise fall through to the normal load path. Parsers work
	//    at the graph level, so file-level filtering for adk-go directories is
	//    handled by parseADKGoDirectoryFiltered below.
	var changed []string
	if flags.since != "" {
		changed, err = changedFiles(flags.since, flags.input)
		if err != nil {
			return 1, fmt.Errorf("resolve --since: %w", err)
		}
		if len(changed) == 0 {
			// Nothing changed — emit an empty report through the normal
			// reporter path so CI still gets a valid artefact.
			return emitFindings(flags, outputFormat, nil)
		}
	}

	// 3. Load and parse graph (optionally restricted to changed files).
	graph, err := loadGraphFiltered(flags.input, inputFormat, workflowParser, changed)
	if err != nil {
		return 1, fmt.Errorf("load graph: %w", err)
	}

	// 4. Run all analysis rules.
	analyzerFactory := factory.NewAnalyzerFactory()
	rules := analyzerFactory.CreateAll()

	orchestrator := application.NewAnalysisOrchestrator()
	findings := orchestrator.Analyze(graph, rules)

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
func loadGraphWithParser(path, inputFormat string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
	return loadGraphFiltered(path, inputFormat, p, nil)
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

	// Directory input.
	if inputFormat != "adk-go" {
		return nil, fmt.Errorf("directory input is only supported for adk-go format; use a single JSON file for json format")
	}

	return parseADKGoDirectoryFiltered(path, p, allow)
}

// parseFile reads a single file and parses it with the given parser.
func parseFile(path string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
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
func parseADKGoDirectory(dir string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
	return parseADKGoDirectoryFiltered(dir, p, nil)
}

// parseADKGoDirectoryFiltered is parseADKGoDirectory with an optional
// allowlist; when non-nil, only files present in allow are parsed.
func parseADKGoDirectoryFiltered(dir string, p application.WorkflowParser, allow []string) (*domain.WorkflowGraph, error) {
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
		if filepath.Ext(path) != ".go" {
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

// changedFiles runs `git diff --name-only <since>..HEAD` from the current
// working directory and returns paths that fall under inputPrefix.
//
// Paths are normalised with filepath.Clean so comparisons against
// filepath.Walk outputs succeed regardless of trailing slashes or `./` prefixes.
// If git is unavailable or the diff fails, an error is returned: silently
// treating --since as a no-op would defeat the purpose of progressive adoption.
func changedFiles(since, inputPrefix string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s..HEAD", since))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s..HEAD: %w", since, err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return []string{}, nil
	}

	prefix := filepath.Clean(inputPrefix)
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
// cleaned-path comparison.
func fileInAllowlist(path string, allow []string) bool {
	clean := filepath.Clean(path)
	for _, a := range allow {
		if a == clean {
			return true
		}
	}
	return false
}
