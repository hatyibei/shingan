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
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	if runtime.GOOS != "windows" {
		// Setpgid lets us SIGKILL the entire process group on Close, capturing
		// any subprocesses langgraph itself might spawn.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
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
		_ = w.kill()
		w.closed.Store(true)
		return nil, fmt.Errorf("python worker: call %q timed out after %s", method, w.timeout)
	case r := <-ch:
		return r.raw, r.err
	}
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
	if !w.closed.CompareAndSwap(false, true) {
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

	// Give the worker a moment to exit gracefully before SIGKILL.
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

// kill SIGKILLs the subprocess group; safe to call repeatedly.
func (w *PythonWorker) kill() error {
	if w.cmd == nil || w.cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return w.cmd.Process.Kill()
	}
	pid := w.cmd.Process.Pid
	// Negative PID = process group; falls back to plain Kill if Setpgid failed.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = w.cmd.Process.Kill()
	}
	return nil
}

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
// installs; otherwise we walk up to the repo root.
const (
	envOverride = "SHINGAN_LANGGRAPH_SHIM"
	shimRel     = "scripts/export_langgraph_server.py"
)

// LocateShim returns the absolute path to the LangGraph shim script.
//
// Resolution order:
//  1. `SHINGAN_LANGGRAPH_SHIM` environment variable (if set and exists).
//  2. The first existing `scripts/export_langgraph_server.py` walking up from
//     the current working directory (covers `go test` and CLI runs).
//  3. The first existing copy under the executable's directory tree (covers
//     vendored installs where binaries ship alongside `scripts/`).
//
// When nothing is found a descriptive error is returned so callers can produce
// actionable diagnostics.
func LocateShim() (string, error) {
	if env := os.Getenv(envOverride); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		return "", fmt.Errorf("langgraph shim: %s=%q points to a non-existent file", envOverride, env)
	}

	if cwd, err := os.Getwd(); err == nil {
		if found, ok := walkUpForShim(cwd); ok {
			return found, nil
		}
	}

	if exe, err := os.Executable(); err == nil {
		if found, ok := walkUpForShim(filepath.Dir(exe)); ok {
			return found, nil
		}
	}

	return "", fmt.Errorf(
		"langgraph shim: could not locate %s; set %s to point at it",
		shimRel, envOverride,
	)
}

// walkUpForShim returns the absolute path to the shim if any ancestor of
// startDir contains scripts/export_langgraph_server.py.
func walkUpForShim(startDir string) (string, bool) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, shimRel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
