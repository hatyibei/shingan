package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/factory"
	"github.com/spf13/cobra"
)

// analyzeFlags holds the parsed flag values for the analyze subcommand.
type analyzeFlags struct {
	input      string
	format     string // input format: "json" or "adk-go"
	output     string // output format: "json" or "markdown"
	outputFile string // output file path (empty = stdout)
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

	// 2. Load and parse graph.
	graph, err := loadGraphWithParser(flags.input, inputFormat, workflowParser)
	if err != nil {
		return 1, fmt.Errorf("load graph: %w", err)
	}

	// 3. Run all analysis rules.
	analyzerFactory := factory.NewAnalyzerFactory()
	rules := analyzerFactory.CreateAll()

	orchestrator := application.NewAnalysisOrchestrator()
	findings := orchestrator.Analyze(graph, rules)

	// 4. Format the output.
	reporterFactory := factory.NewReporterFactory()
	formatter, err := reporterFactory.Create(outputFormat)
	if err != nil {
		return 1, fmt.Errorf("create reporter: %w", err)
	}

	output, err := formatter.Format(findings)
	if err != nil {
		return 1, fmt.Errorf("format findings: %w", err)
	}

	// 5. Write output to stdout or a file.
	if err := writeOutput(flags.outputFile, output); err != nil {
		return 1, fmt.Errorf("write output: %w", err)
	}

	// 6. Determine exit code based on highest severity found.
	return exitCode(findings), nil
}

// loadGraphWithParser loads and parses a WorkflowGraph from path using the given parser.
// For adk-go format with a directory input, all *.go files are walked and their
// nodes and edges are merged into a single graph.
func loadGraphWithParser(path, inputFormat string, p application.WorkflowParser) (*domain.WorkflowGraph, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if !info.IsDir() {
		// Single file.
		return parseFile(path, p)
	}

	// Directory input.
	if inputFormat != "adk-go" {
		return nil, fmt.Errorf("directory input is only supported for adk-go format; use a single JSON file for json format")
	}

	return parseADKGoDirectory(path, p)
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
