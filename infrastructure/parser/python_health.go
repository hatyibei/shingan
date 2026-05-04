package parser

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PythonHealth probes whether a usable Python runtime (and optionally the
// langgraph package) is available to drive ADR-011's LangGraph parser. The
// LSP server (cmd/shingan-lsp) consults this on startup and on a fixed
// heartbeat to decide whether to operate in full or degraded mode (see
// ADR-009).
//
// The probe is intentionally cheap: a single `python3 --version` call. The
// optional langgraph import probe runs only when WithRequireLangGraph is set
// and is meaningful only after Track P (LangGraph parser) lands. Today,
// failure of the langgraph probe is tolerated by callers — the Go-native
// parsers (json, adk-go, samurai) keep working regardless.
//
// Health state is cached for CacheDuration so a flurry of LSP didChange
// notifications does not fork python3 once per keystroke. A single in-flight
// check is allowed at a time (sync.Mutex on RunCheck) so background
// heartbeats never overlap with on-demand checks initiated by the server's
// constructor.
type PythonHealth struct {
	mu              sync.RWMutex
	executable      string        // "python3" by default; injectable for tests
	requireLangGraph bool          // probe `import langgraph` after the version check
	cacheDuration   time.Duration // how long Status() may serve a cached verdict
	checkTimeout    time.Duration // per-probe wall-clock cap

	// Hooks to allow tests to substitute the shell-out without spawning
	// real subprocesses. exec.Command is the production default.
	commandContext func(ctx context.Context, name string, args ...string) *exec.Cmd
	now            func() time.Time

	last        Status
	lastChecked time.Time
}

// Status captures the most recent health probe result. The fields are
// exposed (not just a bool) so callers can surface a precise "limited
// analysis: python not found" diagnostic to users.
type Status struct {
	// Healthy is true iff the most recent probe succeeded fully (including
	// the langgraph import when WithRequireLangGraph is set).
	Healthy bool
	// Reason carries a short human-readable explanation when Healthy is
	// false. It is intended for embedding in an LSP diagnostic; do not log
	// it as a structured key.
	Reason string
	// Version is the trimmed stdout of `python3 --version` ("Python 3.12.1"
	// style) when the version probe succeeded, otherwise empty.
	Version string
	// CheckedAt is the wall-clock time of the underlying probe, or zero
	// when no probe has run yet.
	CheckedAt time.Time
}

// HealthOption mutates a *PythonHealth during construction. Functional
// options keep the constructor signature stable as we add probes later.
type HealthOption func(*PythonHealth)

// WithExecutable overrides the python interpreter path. Useful for tests
// (a mock script) and for environments where users have a non-standard
// `python` binary on PATH.
func WithExecutable(path string) HealthOption {
	return func(h *PythonHealth) { h.executable = path }
}

// WithRequireLangGraph turns the optional `import langgraph` probe into a
// hard requirement: missing langgraph drops the verdict to Healthy=false.
// Today no caller enables this — Track P lands the LangGraph parser later
// and will flip the switch at that point.
func WithRequireLangGraph() HealthOption {
	return func(h *PythonHealth) { h.requireLangGraph = true }
}

// WithCacheDuration overrides how long Status() may serve a cached verdict
// before re-probing. Default is the heartbeat interval (30s).
func WithCacheDuration(d time.Duration) HealthOption {
	return func(h *PythonHealth) { h.cacheDuration = d }
}

// WithCheckTimeout overrides the per-probe wall-clock cap. Default 3s is
// generous: a healthy `python3 --version` returns under 100ms, but the
// first invocation after a long idle can be slower on macOS.
func WithCheckTimeout(d time.Duration) HealthOption {
	return func(h *PythonHealth) { h.checkTimeout = d }
}

// withCommandContext injects a fake exec.Command builder for tests. Kept
// unexported so the production API stays narrow.
func withCommandContext(fn func(ctx context.Context, name string, args ...string) *exec.Cmd) HealthOption {
	return func(h *PythonHealth) { h.commandContext = fn }
}

// withClock injects a deterministic clock for cache-duration tests.
func withClock(fn func() time.Time) HealthOption {
	return func(h *PythonHealth) { h.now = fn }
}

// NewPythonHealth constructs a probe with sensible defaults. Callers should
// call RunCheck() once at startup and rely on Status() afterwards.
func NewPythonHealth(opts ...HealthOption) *PythonHealth {
	h := &PythonHealth{
		executable:     "python3",
		cacheDuration:  30 * time.Second,
		checkTimeout:   3 * time.Second,
		commandContext: exec.CommandContext,
		now:            time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Status returns the most recent verdict. If no probe has run yet, the
// returned Status has Healthy=false and a "not yet probed" reason — this
// keeps the LSP server in a safe degraded mode until the constructor
// finishes the first RunCheck call.
func (h *PythonHealth) Status() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.last.CheckedAt.IsZero() {
		return Status{
			Healthy: false,
			Reason:  "python health not yet probed",
		}
	}
	return h.last
}

// RunCheck performs a fresh probe and updates the cached status. It returns
// the new Status alongside any underlying error so callers (and tests) can
// distinguish "the probe ran and python is missing" from "the probe itself
// failed to run". Both cases end up with Healthy=false; the error channel is
// purely informational.
//
// RunCheck is short-circuited by a CacheDuration cooldown: callers that
// invoke it within CacheDuration of the previous successful check receive
// the cached value without re-running. This is what makes the heartbeat
// cheap and what allows the on-demand "did something change?" check on
// the LSP didChange path to be free.
func (h *PythonHealth) RunCheck(ctx context.Context) (Status, error) {
	h.mu.Lock()
	if !h.last.CheckedAt.IsZero() && h.now().Sub(h.last.CheckedAt) < h.cacheDuration {
		s := h.last
		h.mu.Unlock()
		return s, nil
	}
	h.mu.Unlock()

	probeCtx, cancel := context.WithTimeout(ctx, h.checkTimeout)
	defer cancel()

	status, probeErr := h.probe(probeCtx)

	h.mu.Lock()
	h.last = status
	h.lastChecked = status.CheckedAt
	h.mu.Unlock()

	return status, probeErr
}

// probe is the do-the-actual-work helper, factored out so RunCheck can keep
// the locking discipline obvious.
func (h *PythonHealth) probe(ctx context.Context) (Status, error) {
	now := h.now()

	versionCmd := h.commandContext(ctx, h.executable, "--version")
	versionOut, err := versionCmd.CombinedOutput()
	if err != nil {
		return Status{
			Healthy:   false,
			Reason:    fmt.Sprintf("%s --version failed: %v", h.executable, err),
			CheckedAt: now,
		}, err
	}
	version := strings.TrimSpace(string(versionOut))

	if !strings.HasPrefix(strings.ToLower(version), "python") {
		return Status{
			Healthy:   false,
			Reason:    fmt.Sprintf("%s --version returned unexpected output: %q", h.executable, version),
			Version:   version,
			CheckedAt: now,
		}, errors.New("unexpected version output")
	}

	if h.requireLangGraph {
		importCmd := h.commandContext(ctx, h.executable, "-c", "import langgraph")
		if err := importCmd.Run(); err != nil {
			return Status{
				Healthy:   false,
				Reason:    "langgraph package not importable; install with `pip install langgraph`",
				Version:   version,
				CheckedAt: now,
			}, err
		}
	}

	return Status{
		Healthy:   true,
		Version:   version,
		CheckedAt: now,
	}, nil
}
