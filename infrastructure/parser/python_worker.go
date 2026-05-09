// Package parser provides WorkflowParser implementations for different input formats.
//
// python_worker.go: long-lived Python subprocess wrapper used by the LangGraph
// parser to ferry JSON-RPC requests across the Go ⇄ Python boundary.
//
// Onion layer: infrastructure (process boundary, stdlib only). Domain types
// never leak into this file — callers translate JSON results into
// `domain.WorkflowGraph` themselves.
//
// Wire format
// -----------
// Newline-delimited JSON. Each request is one line on the worker's stdin,
// each response one line on stdout. Stderr is piped through to the Go
// process's stderr unchanged (no log parsing — diagnostics for humans).
//
// Lifecycle
// ---------
// `NewPythonWorker` spawns the worker once. `Call` is goroutine-safe (a single
// mutex serialises requests). `Close` shuts the worker down via the
// `shutdown` JSON-RPC method, then SIGKILLs the process group as a safety net
// (Setpgid above guarantees we kill any langgraph-spawned children too).
package parser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultCallTimeout caps per-request latency. Cold imports (langgraph +
	// user module) can cost >2s on first call; static parses are sub-second.
	defaultCallTimeout = 30 * time.Second

	// pythonWorkerScannerMax is the maximum size of a single response line.
	// Real LangGraph workflows can produce graphs > 64 KiB JSON; we round to
	// 1 MiB to comfortably cover multi-agent supervisors.
	pythonWorkerScannerMax = 1 << 20

	// jsonRPCParseError mirrors the JSON-RPC spec; treated as fatal for the
	// current call but does not kill the worker.
	jsonRPCMethodNotFound = -32601
)

// PythonWorker manages a single long-lived Python subprocess that speaks the
// Shingan JSON-RPC dialect. The struct itself is goroutine-safe.
type PythonWorker struct {
	scriptPath string
	pythonBin  string

	mu      sync.Mutex // protects cmd / stdin / stdout / nextID
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	nextID  uint64
	timeout time.Duration

	closed atomic.Bool
	reaped atomic.Bool
}

// PythonWorkerOption configures a PythonWorker at construction time.
type PythonWorkerOption func(*PythonWorker)

// WithPythonBinary overrides the python executable used for the worker.
// Defaults to ``python3`` (PATH lookup).
func WithPythonBinary(bin string) PythonWorkerOption {
	return func(w *PythonWorker) {
		if bin != "" {
			w.pythonBin = bin
		}
	}
}

// WithCallTimeout overrides the per-call deadline (default 30s).
func WithCallTimeout(d time.Duration) PythonWorkerOption {
	return func(w *PythonWorker) {
		if d > 0 {
			w.timeout = d
		}
	}
}

// NewPythonWorker spawns the shim subprocess and returns a worker handle.
// The script must exist; missing binaries yield a clear error message.
func NewPythonWorker(scriptPath string, opts ...PythonWorkerOption) (*PythonWorker, error) {
	if scriptPath == "" {
		return nil, errors.New("python worker: scriptPath is required")
	}
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("python worker: script %q: %w", scriptPath, err)
	}

	w := &PythonWorker{
		scriptPath: scriptPath,
		pythonBin:  "python3",
		timeout:    defaultCallTimeout,
	}
	for _, opt := range opts {
		opt(w)
	}

	if err := w.spawn(); err != nil {
		return nil, err
	}
	return w, nil
}

// spawn starts the underlying subprocess with stdin/stdout pipes wired up and
// the worker placed in its own process group (so we can kill any children).
// Caller must hold w.mu (or be in construction phase).
func (w *PythonWorker) spawn() error {
	cmd := exec.Command(w.pythonBin, w.scriptPath)
	cmd.Stderr = os.Stderr // forward Python tracebacks for human debugging.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("python worker: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("python worker: stdout pipe: %w", err)
	}
	// setProcessGroup is a no-op on Windows; on POSIX it sets Setpgid so
	// killProcessGroup below can SIGKILL the entire tree on timeout.
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		// Translate the most common failure into actionable text.
		if isExecNotFound(err) {
			return fmt.Errorf("python worker: %q not found in PATH; install Python 3.10+ to use the LangGraph parser", w.pythonBin)
		}
		return fmt.Errorf("python worker: start: %w", err)
	}
	w.cmd = cmd
	w.stdin = stdin
	w.stdout = bufio.NewReaderSize(stdout, pythonWorkerScannerMax)
	return nil
}

// isExecNotFound returns true if err indicates the python binary is missing.
func isExecNotFound(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return true
	}
	// macOS sometimes returns a wrapped *fs.PathError instead.
	return strings.Contains(err.Error(), "executable file not found")
}

// jsonRPCRequest is the on-wire shape sent on the worker's stdin.
type jsonRPCRequest struct {
	ID     uint64      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is the on-wire shape received on the worker's stdout.
type jsonRPCResponse struct {
	ID     uint64           `json:"id"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *jsonRPCErrorObj `json:"error,omitempty"`
}

type jsonRPCErrorObj struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Call performs a single JSON-RPC request/response round-trip with the worker.
// It is goroutine-safe; concurrent callers serialise on the mutex.
func (w *PythonWorker) Call(method string, params interface{}) (json.RawMessage, error) {
	if w.closed.Load() {
		return nil, errors.New("python worker: closed")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	id := atomic.AddUint64(&w.nextID, 1)
	req := jsonRPCRequest{ID: id, Method: method, Params: params}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("python worker: marshal request: %w", err)
	}
	payload = append(payload, '\n')

	ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()

	// Single channel surfaces stdin write errors and stdout decode results.
	type result struct {
		raw json.RawMessage
		err error
	}
	ch := make(chan result, 1)

	go func() {
		if _, werr := w.stdin.Write(payload); werr != nil {
			ch <- result{err: fmt.Errorf("python worker: write: %w", werr)}
			return
		}
		line, rerr := w.stdout.ReadBytes('\n')
		if rerr != nil && len(line) == 0 {
			ch <- result{err: fmt.Errorf("python worker: read: %w", rerr)}
			return
		}
		var resp jsonRPCResponse
		if jerr := json.Unmarshal(line, &resp); jerr != nil {
			ch <- result{err: fmt.Errorf("python worker: decode response %q: %w", string(line), jerr)}
			return
		}
		if resp.ID != id {
			ch <- result{err: fmt.Errorf("python worker: response id mismatch: want %d got %d", id, resp.ID)}
			return
		}
		if resp.Error != nil {
			ch <- result{err: fmt.Errorf("python worker: rpc error %d: %s", resp.Error.Code, resp.Error.Message)}
			return
		}
		ch <- result{raw: resp.Result}
	}()

	select {
	case <-ctx.Done():
		// Force-kill the worker — a hung Python deadlocks the parser otherwise.
		// Also flip the closed flag so subsequent Call()s short-circuit with
		// a clear error instead of writing to a broken stdin and getting
		// EOF/broken-pipe noise (Codex iter4 P1). Callers that want to keep
		// using LangGraph after a timeout must construct a fresh worker.
		//
		// Reap the killed process in a detached goroutine so it doesn't
		// linger as a zombie in long-running LSP/MCP sessions (Codex iter5
		// P2). Close() short-circuits once closed=true is observed, so
		// without this every timeout would accumulate one unreaped child
		// until the Go parent itself exits.
		_ = w.kill()
		w.closed.Store(true)
		go w.reapAfterKill()
		return nil, fmt.Errorf("python worker: call %q timed out after %s", method, w.timeout)
	case r := <-ch:
		return r.raw, r.err
	}
}

// reapAfterKill blocks on cmd.Wait() so the kernel can release the
// killed subprocess's PID. Called from a detached goroutine after the
// timeout branch in Call(); never returns an error to the caller, but
// errors are intentionally swallowed because the worker is already
// known-dead. Idempotent across concurrent invocations: cmd.Wait() can
// only be called once, so we serialise via the existing mu and a
// secondary "reaped" flag.
func (w *PythonWorker) reapAfterKill() {
	if w == nil || w.cmd == nil {
		return
	}
	if !w.reaped.CompareAndSwap(false, true) {
		return
	}
	_ = w.cmd.Wait()
}

// Closed reports whether the worker has been shut down or its subprocess
// has been killed (e.g. by a Call() timeout). The caller (LSP, CLI, MCP)
// inspects this to decide whether to re-spawn a worker before the next
// analysis instead of reusing a dead one (Codex iter4 P1).
func (w *PythonWorker) Closed() bool {
	if w == nil {
		return true
	}
	return w.closed.Load()
}

// Close attempts a clean shutdown then kills the process group.
// Calling Close more than once is safe.
func (w *PythonWorker) Close() error {
	alreadyClosed := !w.closed.CompareAndSwap(false, true)
	if alreadyClosed {
		// closed=true already (e.g. set by a Call() timeout). Make sure
		// the killed subprocess is reaped — Codex iter5 P2 — so we don't
		// leak a zombie when Close() is invoked after a timeout.
		w.reapAfterKill()
		return nil
	}
	// Best-effort polite shutdown.
	w.mu.Lock()
	if w.stdin != nil {
		// We can't reuse Call here (it sets closed), so write directly.
		_, _ = w.stdin.Write([]byte(`{"id":0,"method":"shutdown"}` + "\n"))
		_ = w.stdin.Close()
	}
	w.mu.Unlock()

	// Give the worker a moment to exit gracefully before SIGKILL. Wait()
	// also reaps the child; mark reaped so any concurrent reapAfterKill
	// call short-circuits.
	w.reaped.Store(true)
	doneCh := make(chan error, 1)
	go func() {
		if w.cmd == nil {
			doneCh <- nil
			return
		}
		doneCh <- w.cmd.Wait()
	}()
	select {
	case err := <-doneCh:
		return err
	case <-time.After(2 * time.Second):
		_ = w.kill()
		return <-doneCh
	}
}

// kill SIGKILLs the subprocess group; safe to call repeatedly. Falls
// back to cmd.Process.Kill() when killProcessGroup is a no-op or
// returns an error (Windows / Setpgid-disabled targets).
func (w *PythonWorker) kill() error {
	if w.cmd == nil || w.cmd.Process == nil {
		return nil
	}
	pid := w.cmd.Process.Pid
	if err := killProcessGroup(pid); err != nil {
		_ = w.cmd.Process.Kill()
	}
	return nil
}

// errProcessGroupNotSupported sentinel signals that the platform-
// specific killProcessGroup implementation is not available (Windows
// today). The caller in kill() above falls back to cmd.Process.Kill().
var errProcessGroupNotSupported = errSentinel("process group operations not supported on this platform")

// ErrPythonFrameworkMissing is wrapped by the LangGraph and CrewAI
// parsers' framework-missing errors so callers can distinguish
// "missing dependency" from per-file syntax errors via errors.Is.
// Directory walks should propagate (errors.Is(err, ErrPythonFrameworkMissing))
// rather than swallow these as per-file warnings — a missing dependency
// is a global failure, not a single-file problem (Codex iter4 P2).
var ErrPythonFrameworkMissing = errSentinel("python framework not importable; check pip install")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// healthCheckResult mirrors the shim's health_check JSON shape.
type healthCheckResult struct {
	Status           string `json:"status"`
	LangGraphVersion string `json:"langgraph_version,omitempty"`
	Error            string `json:"error,omitempty"`
	Python           string `json:"python,omitempty"`
}

// HealthCheck issues a `health_check` RPC and returns the worker's status.
func (w *PythonWorker) HealthCheck() (healthCheckResult, error) {
	raw, err := w.Call("health_check", nil)
	if err != nil {
		return healthCheckResult{}, err
	}
	var hc healthCheckResult
	if uerr := json.Unmarshal(raw, &hc); uerr != nil {
		return healthCheckResult{}, fmt.Errorf("python worker: decode health_check: %w", uerr)
	}
	return hc, nil
}

// Resolve the bundled shim path relative to the parser package directory at
// runtime. We accept an explicit override (env var) for unit tests / vendored
// installs; otherwise we walk up to the repo root, and finally fall back to
// extracting the embedded shim shipped inside the binary itself.
//
// shimSearchPaths lists the relative directories where dev / vendored
// installs can ship the shim source. The embedded fallback (shims_embed.go)
// catches the npm-distribution case where neither path exists on disk.
const (
	envOverride         = "SHINGAN_LANGGRAPH_SHIM"
	shimFilenameDefault = "export_langgraph_server.py"
)

var shimSearchDirs = []string{
	"infrastructure/parser/shims", // canonical location after v0.8.1
	"scripts",                     // legacy / fallback for forks tracking older trees
}

// LocateShim returns the absolute path to the LangGraph shim script.
//
// Resolution order:
//  1. `SHINGAN_LANGGRAPH_SHIM` environment variable (if set and exists).
//  2. The first existing copy of the shim under any of `shimSearchDirs`,
//     walking up from the current working directory (covers `go test` and
//     dev CLI runs from inside the repo).
//  3. The first existing copy under the executable's directory tree (covers
//     vendored installs where binaries ship alongside the source tree).
//  4. The shim bundled inside the binary via `//go:embed`, extracted to
//     the user cache (covers the npm `shingan-lint` distribution where
//     no source tree exists at runtime, ADR-013 Codex iter follow-up).
//
// When nothing is found a descriptive error is returned so callers can produce
// actionable diagnostics.
func LocateShim() (string, error) {
	return locateShimNamed(shimFilenameDefault, envOverride, "langgraph shim")
}

// LocateShimNamed returns the absolute path to a named shim script.
// Used by parsers other than LangGraph (e.g. CrewAI) that ship their own
// Python worker. The lookup follows the same logic as LocateShim but
// with a per-shim env-var override derived from the script name
// (e.g. SHINGAN_CREWAI_SHIM for export_crewai_server.py).
func LocateShimNamed(scriptFilename string) (string, error) {
	envName := "SHINGAN_" + envSuffixFromShim(scriptFilename) + "_SHIM"
	return locateShimNamed(scriptFilename, envName, scriptFilename)
}

// envSuffixFromShim derives the uppercase token between "export_" and
// "_server.py" for use in the override env-var name.
//
//	export_crewai_server.py  → CREWAI
//	export_langgraph_server.py → LANGGRAPH
//	custom_shim.py             → CUSTOM_SHIM
func envSuffixFromShim(name string) string {
	trim := strings.TrimSuffix(name, ".py")
	trim = strings.TrimPrefix(trim, "export_")
	trim = strings.TrimSuffix(trim, "_server")
	if trim == "" {
		trim = name
	}
	return strings.ToUpper(strings.ReplaceAll(trim, ".", "_"))
}

func locateShimNamed(filename, envName, label string) (string, error) {
	if env := os.Getenv(envName); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		return "", fmt.Errorf("%s: %s=%q points to a non-existent file", label, envName, env)
	}

	if cwd, err := os.Getwd(); err == nil {
		if found, ok := walkUpForShimNamed(cwd, filename); ok {
			return found, nil
		}
	}

	if exe, err := os.Executable(); err == nil {
		if found, ok := walkUpForShimNamed(filepath.Dir(exe), filename); ok {
			return found, nil
		}
	}

	// Final fallback: extract the embedded shim. This is the path used
	// by the npm-distributed binary (shingan-lint) where neither the
	// source tree nor a vendored scripts/ directory exists.
	if path, err := extractEmbeddedShim(filename); err == nil {
		return path, nil
	}

	return "", fmt.Errorf(
		"%s: could not locate %s in any of %v; set %s to point at it",
		label, filename, shimSearchDirs, envName,
	)
}

// walkUpForShim returns the absolute path to the LangGraph shim if any
// ancestor of startDir contains the canonical shim location.
func walkUpForShim(startDir string) (string, bool) {
	return walkUpForShimNamed(startDir, shimFilenameDefault)
}

// walkUpForShimNamed walks ancestors of startDir looking for any of the
// configured search directories containing `<filename>`.
func walkUpForShimNamed(startDir, filename string) (string, bool) {
	dir := startDir
	for {
		for _, sub := range shimSearchDirs {
			candidate := filepath.Join(dir, sub, filename)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

