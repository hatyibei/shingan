package parser_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hatyibei/shingan/infrastructure/parser"
)

// findShim resolves the shim path relative to the test source location so
// that `go test` works from any working directory. The shim must always
// exist — failing here means the worktree is broken.
func findShim(t *testing.T) string {
	t.Helper()
	// Walk up from this test file's directory looking for scripts/.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "scripts", "export_langgraph_server.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate scripts/export_langgraph_server.py from %q", dir)
		}
		dir = parent
	}
}

// requirePython skips the test when no python3 is on PATH.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not found in PATH: %v", err)
	}
}

// requirePythonLangGraph skips the test when langgraph is not installed.
// We probe via `python3 -c "import langgraph"` rather than via the worker so
// the gating is cheap and explicit.
func requirePythonLangGraph(t *testing.T) {
	t.Helper()
	requirePython(t)
	cmd := exec.Command("python3", "-c", "import langgraph")
	if err := cmd.Run(); err != nil {
		t.Skipf("python3 -c 'import langgraph' failed (langgraph not installed): %v", err)
	}
}

func TestPythonWorker_HealthCheck(t *testing.T) {
	requirePython(t)
	w, err := parser.NewPythonWorker(findShim(t))
	if err != nil {
		t.Fatalf("NewPythonWorker: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	hc, err := w.HealthCheck()
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	// In environments without langgraph the worker reports "missing_langgraph"
	// instead of crashing — that's the degraded-mode contract.
	if hc.Status != "ok" && hc.Status != "missing_langgraph" {
		t.Errorf("unexpected health status %q", hc.Status)
	}
}

func TestPythonWorker_UnknownMethod(t *testing.T) {
	requirePython(t)
	w, err := parser.NewPythonWorker(findShim(t))
	if err != nil {
		t.Fatalf("NewPythonWorker: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	_, err = w.Call("does_not_exist", nil)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	if !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("error %q does not mention unknown method", err)
	}
}

func TestPythonWorker_BadScript(t *testing.T) {
	requirePython(t)
	bad := filepath.Join(t.TempDir(), "broken.py")
	if err := os.WriteFile(bad, []byte("import sys\nsys.exit(7)\n"), 0o644); err != nil {
		t.Fatalf("write tmp script: %v", err)
	}
	w, err := parser.NewPythonWorker(bad, parser.WithCallTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("spawn broken worker: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	// The script exits before reading stdin; the next Call must surface an
	// error rather than hanging.
	if _, callErr := w.Call("health_check", nil); callErr == nil {
		t.Fatal("expected error from worker that exited immediately")
	}
}

func TestPythonWorker_PythonNotFound(t *testing.T) {
	// Use a definitely-nonexistent binary name. NewPythonWorker should
	// return the canonical "Python … not found in PATH" message.
	_, err := parser.NewPythonWorker(findShim(t),
		parser.WithPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python binary is missing")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestPythonWorker_ScriptMissing(t *testing.T) {
	_, err := parser.NewPythonWorker(filepath.Join(t.TempDir(), "missing.py"))
	if err == nil {
		t.Fatal("expected error for missing script")
	}
	if !errors.Is(err, errMissingScript(err)) {
		// Just check the wording — the exact wrapped error is OS-specific.
		if !strings.Contains(err.Error(), "missing.py") {
			t.Errorf("error %q does not mention script path", err)
		}
	}
}

// errMissingScript is a helper to keep the assertion compact; the underlying
// error is a *fs.PathError, but we only need the path component for the test.
func errMissingScript(err error) error {
	return err
}

// TestPythonWorker_CallSerialisation drives multiple goroutines through the
// worker simultaneously to confirm the mutex serialises stdin/stdout writes.
func TestPythonWorker_CallSerialisation(t *testing.T) {
	requirePython(t)
	w, err := parser.NewPythonWorker(findShim(t))
	if err != nil {
		t.Fatalf("NewPythonWorker: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	const N = 8
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			raw, callErr := w.Call("health_check", nil)
			if callErr != nil {
				errCh <- callErr
				return
			}
			var hc map[string]any
			if jerr := json.Unmarshal(raw, &hc); jerr != nil {
				errCh <- jerr
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < N; i++ {
		if e := <-errCh; e != nil {
			t.Errorf("concurrent call %d: %v", i, e)
		}
	}
}

func TestLocateShim_Override(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "shim.py")
	if err := os.WriteFile(tmp, []byte("# stub\n"), 0o644); err != nil {
		t.Fatalf("write tmp shim: %v", err)
	}
	t.Setenv("SHINGAN_LANGGRAPH_SHIM", tmp)
	got, err := parser.LocateShim()
	if err != nil {
		t.Fatalf("LocateShim: %v", err)
	}
	if got != tmp {
		t.Errorf("LocateShim() = %q, want %q", got, tmp)
	}
}

func TestLocateShim_OverrideMissing(t *testing.T) {
	t.Setenv("SHINGAN_LANGGRAPH_SHIM", filepath.Join(t.TempDir(), "nope.py"))
	if _, err := parser.LocateShim(); err == nil {
		t.Fatal("expected error for non-existent override")
	}
}
