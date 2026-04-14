package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	infraFactory "github.com/hatyibei/shingan/infrastructure/factory"
)

// testdataPath returns the absolute path to a file under the project-root
// testdata directory. Using runtime.Caller allows this to work regardless of
// the working directory in which the test is executed.
func testdataPath(name string) string {
	// This file lives at cmd/shingan/main_test.go; root is two levels up.
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "testdata", name)
}

// pipeline runs the full analysis pipeline on the graph loaded from path.
func pipeline(t *testing.T, path string) []domain.Finding {
	t.Helper()
	graph, err := loadGraph(path)
	if err != nil {
		t.Fatalf("loadGraph(%q): %v", path, err)
	}
	rules := infraFactory.NewAnalyzerFactory().CreateAll()
	return application.NewAnalysisOrchestrator().Analyze(graph, rules)
}

// countBySeverity returns critical, warning, info counts.
func countBySeverity(findings []domain.Finding) (critical, warning, info int) {
	for _, f := range findings {
		switch f.Severity {
		case domain.Critical:
			critical++
		case domain.Warning:
			warning++
		case domain.Info:
			info++
		}
	}
	return
}

// TestBuggy_ExitCode2 verifies that buggy.json yields exit code 2 (Critical).
func TestBuggy_ExitCode2(t *testing.T) {
	findings := pipeline(t, testdataPath("buggy.json"))
	if code := exitCode(findings); code != 2 {
		t.Errorf("exitCode = %d, want 2; findings = %+v", code, findings)
	}
}

// TestBuggy_FindingCounts verifies that buggy.json has ≥1 Critical and ≥1 Warning.
func TestBuggy_FindingCounts(t *testing.T) {
	findings := pipeline(t, testdataPath("buggy.json"))
	crit, warn, _ := countBySeverity(findings)
	if crit == 0 {
		t.Errorf("want ≥1 Critical from buggy.json, got 0; all findings: %+v", findings)
	}
	if warn == 0 {
		t.Errorf("want ≥1 Warning from buggy.json, got 0; all findings: %+v", findings)
	}
}

// TestBuggy_AllThreeRulesFire verifies the three required bug categories fire.
func TestBuggy_AllThreeRulesFire(t *testing.T) {
	findings := pipeline(t, testdataPath("buggy.json"))

	fired := map[string]bool{
		"error_handler_checker": false,
		"cycle_detection":       false,
		"unreachable_node":      false,
	}
	for _, f := range findings {
		if _, ok := fired[f.RuleName]; ok {
			fired[f.RuleName] = true
		}
	}
	for rule, ok := range fired {
		if !ok {
			t.Errorf("rule %q did not fire on buggy.json", rule)
		}
	}
}

// TestClean_ExitCode0 verifies that clean.json yields exit code 0.
func TestClean_ExitCode0(t *testing.T) {
	findings := pipeline(t, testdataPath("clean.json"))
	crit, warn, _ := countBySeverity(findings)
	if crit > 0 || warn > 0 {
		t.Errorf("want no Critical/Warning for clean.json, got Critical=%d Warning=%d; findings: %+v",
			crit, warn, findings)
	}
	if code := exitCode(findings); code != 0 {
		t.Errorf("exitCode = %d, want 0", code)
	}
}

// TestExitCode_Logic tests the exitCode function in isolation.
func TestExitCode_Logic(t *testing.T) {
	tests := []struct {
		name     string
		findings []domain.Finding
		want     int
	}{
		{"empty", nil, 0},
		{"info only", []domain.Finding{{Severity: domain.Info}}, 0},
		{"warning", []domain.Finding{{Severity: domain.Warning}}, 1},
		{"critical", []domain.Finding{{Severity: domain.Critical}}, 2},
		{"critical overrides warning", []domain.Finding{
			{Severity: domain.Warning},
			{Severity: domain.Critical},
		}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.findings); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestLoadGraph_InvalidPath verifies an error is returned for missing files.
func TestLoadGraph_InvalidPath(t *testing.T) {
	if _, err := loadGraph("/no/such/file.json"); err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// TestLoadGraph_MalformedJSON verifies an error is returned for invalid JSON.
func TestLoadGraph_MalformedJSON(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(tmp, []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := loadGraph(tmp); err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

// TestExecuteAnalyze_JSONOutput verifies the full pipeline writes valid JSON to
// a file and the exit code matches expectations.
func TestExecuteAnalyze_JSONOutput(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "result.json")
	flags := &analyzeFlags{
		input:      testdataPath("buggy.json"),
		output:     "json",
		outputFile: outPath,
	}

	code, err := executeAnalyze(flags)
	if err != nil {
		t.Fatalf("executeAnalyze: %v", err)
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}

	var report struct {
		Summary struct {
			Total    int `json:"total"`
			Critical int `json:"critical"`
			Warning  int `json:"warning"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse output JSON: %v", err)
	}
	if report.Summary.Critical == 0 {
		t.Errorf("expected ≥1 critical in summary, got 0")
	}
	if report.Summary.Total == 0 {
		t.Errorf("expected ≥1 total findings in summary, got 0")
	}
}

// TestExecuteAnalyze_MarkdownOutput verifies that --output markdown is accepted.
func TestExecuteAnalyze_MarkdownOutput(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "result.md")
	flags := &analyzeFlags{
		input:      testdataPath("buggy.json"),
		output:     "markdown",
		outputFile: outPath,
	}

	code, err := executeAnalyze(flags)
	if err != nil {
		t.Fatalf("executeAnalyze: %v", err)
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if len(data) == 0 {
		t.Error("markdown output is empty")
	}
}

// ─── adk-go E2E tests ────────────────────────────────────────────────────────

// pipelineADKGo runs the full analysis pipeline on an adk-go source file.
func pipelineADKGo(t *testing.T, path string) []domain.Finding {
	t.Helper()
	flags := &analyzeFlags{
		input:      path,
		format:     "adk-go",
		output:     "json",
		outputFile: filepath.Join(t.TempDir(), "out.json"),
	}
	code, err := executeAnalyze(flags)
	if err != nil {
		t.Fatalf("executeAnalyze adk-go(%q): %v", path, err)
	}
	_ = code

	data, err := os.ReadFile(flags.outputFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// Re-run analysis in memory to return findings.
	parserFactory := infraFactory.NewParserFactory()
	p, err := parserFactory.Create("adk-go")
	if err != nil {
		t.Fatalf("create adk-go parser: %v", err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	graph, err := p.Parse(src)
	if err != nil {
		t.Fatalf("parse adk-go: %v", err)
	}
	_ = data
	rules := infraFactory.NewAnalyzerFactory().CreateAll()
	return application.NewAnalysisOrchestrator().Analyze(graph, rules)
}

// TestADKGo_InfiniteLoop_CriticalFinding verifies that infinite_loop.go
// yields at least one Critical finding (loop_guard or cycle_detection).
// v0.2: cycle_detection Severity for sub-agent cycles under a LoopAgent without
// MaxIterations is now Warning; loop_guard emits the Critical finding instead.
func TestADKGo_InfiniteLoop_CriticalFinding(t *testing.T) {
	findings := pipelineADKGo(t, testdataPath("agents/infinite_loop.go"))

	crit, _, _ := countBySeverity(findings)
	if crit == 0 {
		t.Errorf("expected ≥1 Critical finding from infinite_loop.go; findings=%+v", findings)
	}

	// Confirm loop_guard or cycle_detection fired at Critical.
	fired := false
	for _, f := range findings {
		if f.Severity == domain.Critical &&
			(f.RuleName == "cycle_detection" || f.RuleName == "loop_guard") {
			fired = true
			break
		}
	}
	if !fired {
		t.Errorf("neither cycle_detection nor loop_guard Critical found; findings=%+v", findings)
	}
}

// TestADKGo_InfiniteLoop_ExitCode2 verifies executeAnalyze returns exit code 2.
func TestADKGo_InfiniteLoop_ExitCode2(t *testing.T) {
	flags := &analyzeFlags{
		input:      testdataPath("agents/infinite_loop.go"),
		format:     "adk-go",
		output:     "json",
		outputFile: filepath.Join(t.TempDir(), "out.json"),
	}
	code, err := executeAnalyze(flags)
	if err != nil {
		t.Fatalf("executeAnalyze: %v", err)
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// TestExecuteAnalyze_SARIFOutput verifies that --output sarif produces valid SARIF v2.1.0 JSON.
func TestExecuteAnalyze_SARIFOutput(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "result.sarif")
	flags := &analyzeFlags{
		input:      testdataPath("buggy.json"),
		output:     "sarif",
		outputFile: outPath,
	}

	code, err := executeAnalyze(flags)
	if err != nil {
		t.Fatalf("executeAnalyze: %v", err)
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v", err)
	}
	if doc["version"] != "2.1.0" {
		t.Errorf("SARIF version = %q, want \"2.1.0\"", doc["version"])
	}
	if doc["$schema"] != "https://json.schemastore.org/sarif-2.1.0.json" {
		t.Errorf("SARIF $schema = %q", doc["$schema"])
	}
	runs := doc["runs"].([]interface{})
	run := runs[0].(map[string]interface{})
	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})
	if driver["name"] != "Shingan" {
		t.Errorf("driver.name = %q, want \"Shingan\"", driver["name"])
	}
	results := run["results"].([]interface{})
	if len(results) == 0 {
		t.Error("expected ≥1 SARIF result for buggy.json, got 0")
	}
}

// TestADKGo_Directory_MergesAllFiles verifies directory-mode merges nodes from all fixture files.
func TestADKGo_Directory_MergesAllFiles(t *testing.T) {
	flags := &analyzeFlags{
		input:      testdataPath("agents"),
		format:     "adk-go",
		output:     "json",
		outputFile: filepath.Join(t.TempDir(), "out.json"),
	}
	code, err := executeAnalyze(flags)
	if err != nil {
		t.Fatalf("executeAnalyze dir: %v", err)
	}
	// Should have Critical findings (cycle from infinite_loop.go).
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (expected Critical findings from merged graph)", code)
	}
}
