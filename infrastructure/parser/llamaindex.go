// Package parser provides WorkflowParser implementations for different input formats.
//
// llamaindex.go: parser that ferries LlamaIndex Workflows (the
// `llama_index.core.workflow` event-driven API) definitions across the
// Go ⇄ Python boundary via a long-lived JSON-RPC worker
// (`shims/export_llamaindex_server.py`).
//
// Onion layer: infrastructure. The Go side knows nothing about Python AST or
// LlamaIndex internals — every framework-specific concern lives in the shim.
// See ADR-015 (framework-by-framework PoC parsers; LlamaIndex Workflows is #4
// after LangGraph.js and pydantic-graph) and ADR-009 for the
// long-lived-worker / degraded-mode pattern.
//
// The Python shim is AST-only: it parses .py via the stdlib `ast` module and
// never imports `llama_index`, so the worker is healthy whenever Python loads
// the shim. Unlike the LangGraph / CrewAI parsers there is no "framework not
// installed" runtime gate — `python3` alone is sufficient, exactly like the
// pydantic-graph + LangGraph.js shims. This also sidesteps the ADR-014
// runtime-introspection trap.
//
// Resource ownership
// ------------------
// `LlamaIndexParser` owns one `PythonWorker`. Callers are expected to invoke
// `Close()` when done; tests/CLIs short-circuit by deferring it. ParserFactory
// stores a single instance per analysis run, matching the v0.6 LSP design.
package parser

import (
	"fmt"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// llamaIndexShimFilename is the bundled Python shim that exports LlamaIndex
// Workflows via stdlib `ast`.
const llamaIndexShimFilename = "export_llamaindex_server.py"

// LlamaIndexParser converts LlamaIndex Workflows Python source into a Shingan
// WorkflowGraph by delegating to a long-lived Python worker. The worker is the
// same PythonWorker implementation that LangGraphParser / CrewAIParser /
// PydanticGraphParser use; only the shim script differs (per ADR-015).
type LlamaIndexParser struct {
	worker *PythonWorker

	mu       sync.Mutex
	healthOK bool
	healthCk bool
}

// LlamaIndexOption configures a LlamaIndexParser at construction time.
type LlamaIndexOption func(*llamaIndexConfig)

type llamaIndexConfig struct {
	scriptPath     string
	pythonBin      string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
}

// WithLlamaIndexScriptPath overrides the path to the shim Python script.
// Default: locates `export_llamaindex_server.py` via LocateShimNamed.
func WithLlamaIndexScriptPath(path string) LlamaIndexOption {
	return func(c *llamaIndexConfig) { c.scriptPath = path }
}

// WithLlamaIndexPythonBinary overrides the Python interpreter used for the
// underlying worker. Default: "python3".
func WithLlamaIndexPythonBinary(bin string) LlamaIndexOption {
	return func(c *llamaIndexConfig) { c.pythonBin = bin }
}

// WithLlamaIndexWorker reuses a pre-constructed PythonWorker (for tests).
func WithLlamaIndexWorker(w *PythonWorker) LlamaIndexOption {
	return func(c *llamaIndexConfig) { c.existingWorker = w }
}

// NewLlamaIndexParser instantiates the parser and (unless WithLlamaIndexWorker
// is supplied) spawns the underlying Python subprocess. The returned parser
// must be `Close()`d to release process resources.
func NewLlamaIndexParser(opts ...LlamaIndexOption) (*LlamaIndexParser, error) {
	cfg := &llamaIndexConfig{
		pythonBin: "python3",
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.existingWorker != nil {
		return &LlamaIndexParser{worker: cfg.existingWorker}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShimNamed(llamaIndexShimFilename)
		if err != nil {
			return nil, fmt.Errorf("llamaindex parser: %w", err)
		}
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.pythonBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.pythonBin))
	}
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("llamaindex parser: %w", err)
	}
	return &LlamaIndexParser{worker: worker}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *LlamaIndexParser) SupportedFormat() string { return "llamaindex" }

// Parse converts inline Python source into a WorkflowGraph by sending it to
// the worker via `parse_content`. The synthetic filename "<inline.py>" is
// used because callers of this entry point do not have a real path on disk.
func (p *LlamaIndexParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.py>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to the
// Python worker. The shim records it as the node SourcePos.File; the AST-only
// strategy never needs sibling-import resolution, so the hint is purely for
// diagnostics here (parity with the other parsers' signatures).
func (p *LlamaIndexParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
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
		return nil, fmt.Errorf("llamaindex parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to read the file from disk and export its
// LlamaIndex Workflow into Shingan's WorkflowGraph JSON shape. Implements the
// fileParser interface so the CLI directory walk reads .py files directly.
func (p *LlamaIndexParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	raw, err := p.worker.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("llamaindex parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// Close releases the underlying Python worker.
func (p *LlamaIndexParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying Python worker has been shut down or
// killed (e.g. by a Call() timeout).
func (p *LlamaIndexParser) Closed() bool {
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
func (p *LlamaIndexParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errLlamaIndexUnhealthy
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("llamaindex parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errLlamaIndexUnhealthy
	}
	p.healthOK = true
	return nil
}

// errLlamaIndexUnhealthy is surfaced when the Python worker is reachable but
// reports a non-"ok" health status (e.g. the shim failed to load). It wraps
// ErrPythonFrameworkMissing for symmetry with the other parsers so directory
// walks treat it as a global (not per-file) failure. The shim never imports
// llama_index, so in practice this only fires on a broken Python.
var errLlamaIndexUnhealthy = fmt.Errorf(
	"llamaindex parser: Python 3.x required for llamaindex format (the AST shim failed to load): %w",
	ErrPythonFrameworkMissing,
)
