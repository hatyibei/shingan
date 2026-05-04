package parser

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakePython returns an *exec.Cmd that, when run, prints `output` to stdout
// (or stderr when toStderr is true) and exits with the given exit code.
// We rely on /bin/sh's built-in printf which is portable across Linux/macOS;
// the few tests that need this are skipped on Windows in CI.
func fakePython(t *testing.T, output string, exitCode int, toStderr bool) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fakePython relies on /bin/sh; not portable to Windows runners")
	}
	stream := "1" // stdout
	if toStderr {
		stream = "2"
	}
	script := "printf '%s\\n' \"" + output + "\" >&" + stream + "; exit " + intToStr(exitCode)
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	return strings.TrimSpace(string(rune('0' + i)))
}

func TestPythonHealth_StatusBeforeFirstCheck(t *testing.T) {
	t.Parallel()

	h := NewPythonHealth()
	status := h.Status()
	if status.Healthy {
		t.Fatalf("expected unhealthy before first probe; got %+v", status)
	}
	if status.Reason == "" {
		t.Fatalf("expected an explanatory reason on a fresh probe")
	}
}

func TestPythonHealth_RunCheck_Healthy(t *testing.T) {
	t.Parallel()

	h := NewPythonHealth(
		withCommandContext(fakePython(t, "Python 3.12.1", 0, false)),
	)
	status, err := h.RunCheck(context.Background())
	if err != nil {
		t.Fatalf("RunCheck returned error: %v", err)
	}
	if !status.Healthy {
		t.Fatalf("expected Healthy=true; got %+v", status)
	}
	if !strings.HasPrefix(status.Version, "Python ") {
		t.Fatalf("expected Version to start with 'Python '; got %q", status.Version)
	}
	if status.CheckedAt.IsZero() {
		t.Fatalf("expected CheckedAt to be populated")
	}
}

func TestPythonHealth_RunCheck_NoBinary(t *testing.T) {
	t.Parallel()

	// Use a deliberately bogus executable name; exec.Command will fail
	// to start with err != nil, so the probe must report unhealthy.
	h := NewPythonHealth(WithExecutable("/nonexistent/python-binary-xyz"))
	status, err := h.RunCheck(context.Background())
	if err == nil {
		t.Fatalf("expected an underlying err; got nil")
	}
	if status.Healthy {
		t.Fatalf("expected Healthy=false; got %+v", status)
	}
	if status.Reason == "" {
		t.Fatalf("expected a non-empty reason on failure")
	}
}

func TestPythonHealth_RunCheck_UnexpectedOutput(t *testing.T) {
	t.Parallel()

	// Simulate a binary on PATH whose --version output does NOT start
	// with "Python" — could be `cat`, `which`, etc. The probe must
	// reject this.
	h := NewPythonHealth(
		withCommandContext(fakePython(t, "BusyBox v1.36", 0, false)),
	)
	status, err := h.RunCheck(context.Background())
	if err == nil {
		t.Fatalf("expected an unexpected-output error; got nil")
	}
	if status.Healthy {
		t.Fatalf("expected Healthy=false; got %+v", status)
	}
	if !strings.Contains(status.Reason, "unexpected output") {
		t.Fatalf("expected reason to mention 'unexpected output'; got %q", status.Reason)
	}
}

func TestPythonHealth_RunCheck_Cached(t *testing.T) {
	t.Parallel()

	calls := 0
	cmd := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls++
		return fakePython(t, "Python 3.11.5", 0, false)(ctx, name, args...)
	}

	clock := time.Unix(1_700_000_000, 0)
	h := NewPythonHealth(
		WithCacheDuration(time.Minute),
		withCommandContext(cmd),
		withClock(func() time.Time { return clock }),
	)

	if _, err := h.RunCheck(context.Background()); err != nil {
		t.Fatalf("first RunCheck failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 probe; got %d", calls)
	}

	// Second call within cache window: must NOT spawn a new subprocess.
	clock = clock.Add(10 * time.Second)
	if _, err := h.RunCheck(context.Background()); err != nil {
		t.Fatalf("cached RunCheck failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected cache hit; got %d probes", calls)
	}

	// Advance past cache window: probe runs again.
	clock = clock.Add(2 * time.Minute)
	if _, err := h.RunCheck(context.Background()); err != nil {
		t.Fatalf("post-expiry RunCheck failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected re-probe after cache expiry; got %d probes", calls)
	}
}

func TestPythonHealth_StatusReflectsLastProbe(t *testing.T) {
	t.Parallel()

	h := NewPythonHealth(
		withCommandContext(fakePython(t, "Python 3.10.4", 0, false)),
	)
	if _, err := h.RunCheck(context.Background()); err != nil {
		t.Fatalf("RunCheck failed: %v", err)
	}
	got := h.Status()
	if !got.Healthy {
		t.Fatalf("expected Status to report healthy after successful probe")
	}
}
