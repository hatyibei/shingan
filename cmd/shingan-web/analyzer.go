package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

// analyzeAgentSource runs all Shingan rules on the Go source file associated
// with the given agent name. It returns the full slice of findings (sorted by
// severity descending) and an error only for unexpected failures.
//
// If the agent name is not in the sourceMap, an error is returned.
func analyzeAgentSource(agentName string, sourceMap map[string]string) ([]domain.Finding, error) {
	relPath, ok := sourceMap[agentName]
	if !ok {
		// Unknown agent — no source to analyze, allow through.
		return nil, fmt.Errorf("no source file registered for agent %q", agentName)
	}

	absPath := resolveSourcePath(relPath) // handles both relative and absolute paths

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read source %q: %w", absPath, err)
	}

	parserFac := factory.NewParserFactory()
	p, err := parserFac.Create("adk-go")
	if err != nil {
		return nil, fmt.Errorf("create adk-go parser: %w", err)
	}

	graph, err := p.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", absPath, err)
	}

	analyzerFac := factory.NewAnalyzerFactory()
	rules := analyzerFac.CreateAll()

	orch := application.NewAnalysisOrchestrator()
	return orch.Analyze(graph, rules), nil
}

// hasCritical returns true when any finding in the slice is Critical severity.
func hasCritical(findings []domain.Finding) bool {
	for _, f := range findings {
		if f.Severity == domain.Critical {
			return true
		}
	}
	return false
}

// resolveSourcePath converts a path to an absolute path. If the path is already
// absolute, it is returned as-is. If it is relative, it is resolved against
// the project root (determined by walking up to find go.mod).
func resolveSourcePath(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	root := findProjectRoot()
	return filepath.Join(root, rel)
}

// findProjectRoot walks up from cwd looking for go.mod.
func findProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

// lookupEnv wraps os.LookupEnv for testability.
func lookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
