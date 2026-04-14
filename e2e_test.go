//go:build e2e

// Package shingan_e2e contains end-to-end smoke tests for the shingan CLI and API.
// Run with: go test -tags=e2e -race ./...
package shingan_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/application"
	infraapi "github.com/hatyibei/shingan/infrastructure/api"
	"github.com/hatyibei/shingan/infrastructure/factory"

	analyzer "github.com/hatyibei/shingan/gen/analyzer"
	genserver "github.com/hatyibei/shingan/gen/http/analyzer/server"
	goahttp "goa.design/goa/v3/http"
)

// cliBinary is the path to the compiled shingan CLI binary, built once in TestMain.
var cliBinary string

// testdataDir is the absolute path to the testdata directory.
var testdataDir string

func TestMain(m *testing.M) {
	// Locate project root via this file's location.
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(file)
	testdataDir = filepath.Join(root, "testdata")

	// Build the CLI binary into a temp file.
	bin := filepath.Join(os.TempDir(), "shingan-e2e-cli")
	build := exec.Command("go", "build", "-o", bin, "./cmd/shingan")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		panic("failed to build shingan CLI: " + string(out))
	}
	cliBinary = bin

	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
}

// runCLI runs the shingan CLI with given args and returns stdout, stderr, exit code.
func runCLI(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command(cliBinary, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return
}

// newAPIServer builds an httptest.Server with the goa handler mounted.
func newAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	pf := factory.NewParserFactory()
	af := factory.NewAnalyzerFactory()
	orch := application.NewAnalysisOrchestrator()
	svc := infraapi.NewAnalyzerService(pf, af, orch)
	endpoints := analyzer.NewEndpoints(svc)
	mux := goahttp.NewMuxer()
	srv := genserver.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	srv.Mount(mux)
	return httptest.NewServer(mux)
}

// ─── CLI Tests ─────────────────────────────────────────────────────────────────

// TestE2E_CLI_JSON_BuggyExitCode2 verifies buggy.json yields exit code 2.
func TestE2E_CLI_JSON_BuggyExitCode2(t *testing.T) {
	input := filepath.Join(testdataDir, "buggy.json")
	stdout, stderr, code := runCLI("analyze", "--format", "json", "--input", input, "--output", "json")
	t.Logf("stdout=%s stderr=%s", stdout, stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	// Verify JSON output is parseable and has findings.
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Errorf("stdout is not valid JSON: %v\nstdout=%s", err, stdout)
	}
}

// TestE2E_CLI_JSON_CleanExitCode0 verifies clean.json yields exit code 0.
func TestE2E_CLI_JSON_CleanExitCode0(t *testing.T) {
	input := filepath.Join(testdataDir, "clean.json")
	stdout, stderr, code := runCLI("analyze", "--format", "json", "--input", input, "--output", "json")
	t.Logf("stdout=%s stderr=%s", stdout, stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

// TestE2E_CLI_ADKGo_InfiniteLoop_Critical verifies infinite_loop.go → exit code 2.
func TestE2E_CLI_ADKGo_InfiniteLoop_Critical(t *testing.T) {
	input := filepath.Join(testdataDir, "agents", "infinite_loop.go")
	stdout, stderr, code := runCLI("analyze", "--format", "adk-go", "--input", input, "--output", "json")
	t.Logf("stdout=%s stderr=%s", stdout, stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (critical finding expected)", code)
	}
	// Check cycle_detection fired.
	if !strings.Contains(stdout, "cycle_detection") {
		t.Errorf("expected cycle_detection in output; stdout=%s", stdout)
	}
}

// TestE2E_CLI_ADKGo_Directory_AllFiles verifies directory input merges all 3 fixture files.
func TestE2E_CLI_ADKGo_Directory_AllFiles(t *testing.T) {
	input := filepath.Join(testdataDir, "agents")
	stdout, stderr, code := runCLI("analyze", "--format", "adk-go", "--input", input, "--output", "json")
	t.Logf("stdout=%s stderr=%s", stdout, stderr)
	// Directory includes infinite_loop.go → Critical.
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (critical from merged graph)", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Errorf("stdout is not valid JSON: %v", err)
		return
	}
	findings, _ := result["findings"].([]any)
	if len(findings) == 0 {
		t.Error("expected at least one finding from directory analysis")
	}
}

// TestE2E_CLI_MarkdownOutput verifies --output markdown produces markdown content.
func TestE2E_CLI_MarkdownOutput(t *testing.T) {
	input := filepath.Join(testdataDir, "buggy.json")
	stdout, stderr, _ := runCLI("analyze", "--format", "json", "--input", input, "--output", "markdown")
	t.Logf("stderr=%s", stderr)
	// Markdown output must contain ## headers.
	if !strings.Contains(stdout, "##") && !strings.Contains(stdout, "#") {
		t.Errorf("expected markdown headers in output; stdout=%q", stdout)
	}
	// Must contain severity indicators.
	hasCritical := strings.Contains(stdout, "Critical") || strings.Contains(stdout, "critical")
	if !hasCritical {
		t.Errorf("expected 'Critical' in markdown output; stdout=%q", stdout)
	}
}

// ─── API Tests ─────────────────────────────────────────────────────────────────

// TestE2E_API_Healthz verifies GET /healthz → 200 "ok".
func TestE2E_API_Healthz(t *testing.T) {
	ts := newAPIServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var s string
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal health: %v; body=%s", err, body)
	}
	if s != "ok" {
		t.Fatalf("expected %q, got %q", "ok", s)
	}
}

// TestE2E_API_AnalyzeJSON verifies POST /analyze with buggy.json → 200 + findings.
func TestE2E_API_AnalyzeJSON(t *testing.T) {
	ts := newAPIServer(t)
	defer ts.Close()

	content, err := os.ReadFile(filepath.Join(testdataDir, "buggy.json"))
	if err != nil {
		t.Fatalf("read buggy.json: %v", err)
	}

	b64 := base64.StdEncoding.EncodeToString(content)
	reqBody := map[string]any{
		"format":  "json",
		"content": b64,
		"output":  "json",
	}
	buf, _ := json.Marshal(reqBody)

	resp, err := http.Post(ts.URL+"/analyze", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /analyze: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	findings, ok := result["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Errorf("expected at least one finding; result=%v", result)
	}

	summary, _ := result["summary"].(map[string]any)
	if summary == nil {
		t.Error("expected summary in response")
	} else {
		critical, _ := summary["critical"].(float64)
		if critical == 0 {
			t.Errorf("expected ≥1 critical in summary; summary=%v", summary)
		}
	}

	// Verify exit_code.
	exitCode, _ := result["exit_code"].(float64)
	if exitCode != 2 {
		t.Errorf("expected exit_code=2, got %v", exitCode)
	}
	t.Logf("findings=%d exit_code=%v", len(findings), exitCode)
}

// TestE2E_API_AnalyzeADKGo verifies POST /analyze with adk-go content returns findings.
func TestE2E_API_AnalyzeADKGo(t *testing.T) {
	ts := newAPIServer(t)
	defer ts.Close()

	content, err := os.ReadFile(filepath.Join(testdataDir, "agents", "infinite_loop.go"))
	if err != nil {
		t.Fatalf("read infinite_loop.go: %v", err)
	}

	ctx := context.Background()
	_ = ctx

	b64 := base64.StdEncoding.EncodeToString(content)
	reqBody := map[string]any{
		"format":  "adk-go",
		"content": b64,
		"output":  "json",
	}
	buf, _ := json.Marshal(reqBody)

	resp, err := http.Post(ts.URL+"/analyze", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /analyze adk-go: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	findings, _ := result["findings"].([]any)
	if len(findings) == 0 {
		t.Errorf("expected findings for infinite_loop.go; result=%v", result)
	}
	t.Logf("adk-go findings: %d", len(findings))
}
