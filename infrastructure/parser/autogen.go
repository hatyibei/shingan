// Package parser provides WorkflowParser implementations for different input formats.
//
// autogen.go: parser that ferries Microsoft AutoGen GraphFlow workflow
// definitions (the `autogen_agentchat` `DiGraphBuilder` / `GraphFlow` API)
// across the Go ⇄ Python boundary via a long-lived JSON-RPC worker
// (`shims/export_autogen_server.py`).
//
// Onion layer: infrastructure. The Go side knows nothing about Python AST or
// AutoGen internals — every framework-specific concern lives in the shim. See
// ADR-015 (framework-by-framework PoC parsers) and ADR-009 for the
// long-lived-worker / degraded-mode pattern.
//
// The Python shim is AST-only: it parses .py via the stdlib `ast` module and
// never imports `autogen_agentchat`, so the worker is healthy whenever Python
// loads the shim. Like the pydantic-graph / LangGraph.js parsers there is no
// "framework not installed" runtime gate — `python3` alone is sufficient. This
// also sidesteps the ADR-014 runtime-introspection trap.
//
// No exit sentinel
// ----------------
// Unlike LangGraph (`END`), pydantic-graph (`End`) and LlamaIndex
// (`StopEvent`), AutoGen GraphFlow terminates via EXTERNAL termination
// conditions passed to `GraphFlow`, not an in-graph sentinel. The shim invents
// no sentinel: cycle bounding relies on shingan's structural cycle-exit
// (domain/rules/cycle.go `cycleHasExit`), which downgrades a cycle to Warning
// when some in-cycle node has an edge leaving the cycle to another real node.
//
// Resource ownership
// ------------------
// `AutoGenParser` owns one `PythonWorker`. Callers are expected to invoke
// `Close()` when done; tests/CLIs short-circuit by deferring it. ParserFactory
// stores a single instance per analysis run, matching the v0.6 LSP design.
package parser

import (
	"fmt"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// autoGenShimFilename is the bundled Python shim that exports AutoGen GraphFlow
// workflows via stdlib `ast`.
const autoGenShimFilename = "export_autogen_server.py"

// AutoGenParser converts AutoGen GraphFlow Python source into a Shingan
// WorkflowGraph by delegating to a long-lived Python worker. The worker is the
// same PythonWorker implementation that the LangGraph / CrewAI / pydantic-graph
// parsers use; only the shim script differs (per ADR-015).
type AutoGenParser struct {
	worker *PythonWorker

	mu       sync.Mutex
	healthOK bool
	healthCk bool
}

// AutoGenOption configures an AutoGenParser at construction time.
type AutoGenOption func(*autoGenConfig)

type autoGenConfig struct {
	scriptPath     string
	pythonBin      string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
}

// WithAutoGenScriptPath overrides the path to the shim Python script.
// Default: locates `export_autogen_server.py` via LocateShimNamed.
func WithAutoGenScriptPath(path string) AutoGenOption {
	return func(c *autoGenConfig) { c.scriptPath = path }
}

// WithAutoGenPythonBinary overrides the Python interpreter used for the
// underlying worker. Default: "python3".
func WithAutoGenPythonBinary(bin string) AutoGenOption {
	return func(c *autoGenConfig) { c.pythonBin = bin }
}

// WithAutoGenWorker reuses a pre-constructed PythonWorker (for tests).
func WithAutoGenWorker(w *PythonWorker) AutoGenOption {
	return func(c *autoGenConfig) { c.existingWorker = w }
}

// NewAutoGenParser instantiates the parser and (unless WithAutoGenWorker is
// supplied) spawns the underlying Python subprocess. The returned parser must
// be `Close()`d to release process resources.
func NewAutoGenParser(opts ...AutoGenOption) (*AutoGenParser, error) {
	cfg := &autoGenConfig{
		pythonBin: "python3",
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.existingWorker != nil {
		return &AutoGenParser{worker: cfg.existingWorker}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShimNamed(autoGenShimFilename)
		if err != nil {
			return nil, fmt.Errorf("autogen parser: %w", err)
		}
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.pythonBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.pythonBin))
	}
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("autogen parser: %w", err)
	}
	return &AutoGenParser{worker: worker}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *AutoGenParser) SupportedFormat() string { return "autogen" }

// Parse converts inline Python source into a WorkflowGraph by sending it to
// the worker via `parse_content`. The synthetic filename "<inline.py>" is
// used because callers of this entry point do not have a real path on disk.
func (p *AutoGenParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.py>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to the
// Python worker. The shim records it as the node SourcePos.File; the AST-only
// strategy never needs sibling-import resolution, so the hint is purely for
// diagnostics here (parity with the other parsers' signatures).
func (p *AutoGenParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = "<inline.py>"
	}
	raw, err := p.worker.Call("parse_content", map[string]string{
		"content":  string(input),
		"filename": filename,
	})
	if err != nil {
		return nil, fmt.Errorf("autogen parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to read the file from disk and export its AutoGen
// GraphFlow workflow into Shingan's WorkflowGraph JSON shape. Implements the
// fileParser interface so the CLI directory walk reads .py files directly.
func (p *AutoGenParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	raw, err := p.worker.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("autogen parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// Close releases the underlying Python worker.
func (p *AutoGenParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying Python worker has been shut down or
// killed (e.g. by a Call() timeout).
func (p *AutoGenParser) Closed() bool {
	if p == nil || p.worker == nil {
		return true
	}
	return p.worker.Closed()
}

// ensureHealthy lazily runs a health_check on first use. The check is memoised
// so failing fast on the same parser is the desired behaviour. The shim is
// AST-only and imports no framework, so this gate reports status "ok" whenever
// Python itself loaded the shim — it effectively only fails when `python3` is
// broken or the shim file is corrupt.
func (p *AutoGenParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errAutoGenUnhealthy
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("autogen parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errAutoGenUnhealthy
	}
	p.healthOK = true
	return nil
}

// errAutoGenUnhealthy is surfaced when the Python worker is reachable but
// reports a non-"ok" health status (e.g. the shim failed to load). It wraps
// ErrPythonFrameworkMissing for symmetry with the other parsers so directory
// walks treat it as a global (not per-file) failure. The shim never imports
// autogen_agentchat, so in practice this only fires on a broken Python.
var errAutoGenUnhealthy = fmt.Errorf(
	"autogen parser: Python 3.x required for autogen format (the AST shim failed to load): %w",
	ErrPythonFrameworkMissing,
)
