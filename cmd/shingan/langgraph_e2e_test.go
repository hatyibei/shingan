package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// langgraphTestdataPath returns the absolute path to a file under
// `testdata/langgraph/`.
func langgraphTestdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "testdata", "langgraph", name)
}

// shimPath returns the absolute path to scripts/export_langgraph_server.py.
func shimPath() string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "scripts", "export_langgraph_server.py")
}

// requirePythonLangGraphE2E skips when python3 + langgraph aren't both
// available in the test environment. The `shingan analyze` binary itself is
// fine without them, but loading user `.py` files needs both.
func requirePythonLangGraphE2E(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not found in PATH: %v", err)
	}
	cmd := exec.Command("python3", "-c", "import langgraph")
	if err := cmd.Run(); err != nil {
		t.Skipf("langgraph not importable: %v", err)
	}
}

// buildShinganBinary compiles `shingan` into a temp directory and returns the
// absolute path. We invoke `go build` rather than execing through `go run`
// so the resulting subprocess speaks the same exit codes as a real install.
func buildShinganBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "shingan")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/shingan")
	// Run from project root so module resolution works.
	_, file, _, _ := runtime.Caller(0)
	cmd.Dir = filepath.Join(filepath.Dir(file), "..", "..")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build shingan: %v\n%s", err, stderr.String())
	}
	return out
}

func TestLangGraphE2E_ReactLoop_Critical(t *testing.T) {
	requirePythonLangGraphE2E(t)
	bin := buildShinganBinary(t)
	cmd := exec.Command(bin,
		"analyze",
		"--format", "langgraph",
		"--input", langgraphTestdataPath("react_loop.py"),
		"--output", "json",
	)
	cmd.Env = append(cmd.Environ(), "SHINGAN_LANGGRAPH_SHIM="+shimPath())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// react_loop has a model⇄tools cycle with no max_iterations → expect
	// either Critical (exit 2) or at least Warning (exit 1). We accept both
	// because Phase 1 may report the cycle as Warning depending on the rule
	// version. Plain success (exit 0) is a regression.
	if err == nil {
		t.Fatalf("expected non-zero exit, got success.\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("unexpected error type: %v\nstderr=%s", err, stderr.String())
	}
	code := exitErr.ExitCode()
	if code < 1 {
		t.Fatalf("exit code = %d, want >= 1\nstdout=%s\nstderr=%s", code, stdout.String(), stderr.String())
	}

	// Sanity-check the JSON report contains a finding.
	var report struct {
		Findings []map[string]any `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		// Older reporter may emit a different envelope; just assert that the
		// substring "cycle" appears somewhere in the output.
		if !strings.Contains(strings.ToLower(stdout.String()), "cycle") &&
			!strings.Contains(strings.ToLower(stdout.String()), "loop") {
			t.Errorf("output does not mention cycle/loop: %s", stdout.String())
		}
		return
	}
	if len(report.Findings) == 0 {
		t.Errorf("expected at least one finding, got 0; output=%s", stdout.String())
	}
}

func TestLangGraphE2E_SimpleChain_Clean(t *testing.T) {
	requirePythonLangGraphE2E(t)
	bin := buildShinganBinary(t)
	cmd := exec.Command(bin,
		"analyze",
		"--format", "langgraph",
		"--input", langgraphTestdataPath("simple_chain.py"),
		"--output", "json",
	)
	cmd.Env = append(cmd.Environ(), "SHINGAN_LANGGRAPH_SHIM="+shimPath())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected error: %v\nstderr=%s", err, stderr.String())
		}
		// Exit 1 (Warning) is acceptable — heuristics may legitimately fire on
		// the simple chain. Exit 2 (Critical) is a regression.
		if exitErr.ExitCode() == 2 {
			t.Errorf("simple_chain should not produce Critical findings\nstdout=%s\nstderr=%s",
				stdout.String(), stderr.String())
		}
	}
}
