// Package parser provides WorkflowParser implementations for different input formats.
//
// mastra.go: parser that ferries Mastra (mastra.ai) workflow definitions
// (the `@mastra/core/workflows` `createStep` / `createWorkflow` fluent-chain
// API) across the Go ⇄ Node boundary via a long-lived JSON-RPC worker
// (`shims/export_mastra_server.mjs`).
//
// Onion layer: infrastructure. The Go side knows nothing about TypeScript AST
// or Mastra internals — every framework-specific concern lives in the bundled
// Node shim. See ADR-015 (framework-by-framework PoC parsers) and ADR-009 for
// the long-lived-worker / degraded-mode pattern.
//
// The Node shim is AST-only: it parses .ts/.js via the TypeScript Compiler API
// and never imports @mastra/core, so the worker is healthy whenever Node + the
// bundled parser load. Like the LangGraph.js / AutoGen parsers there is no
// "framework not installed" runtime gate — `node` alone is sufficient.
//
// Reuse
// -----
// This parser deliberately reuses the LangGraph.js Node infrastructure verbatim:
// the same subprocess worker (run with the Node binary), the same
// `ensureLangGraphJSDeps` TypeScript bootstrap (the `typescript` dependency is
// shared — there is ONE shims/package.json), and the same `decodeShimGraph`
// wire decode. Only the shim script (`export_mastra_server.mjs`) and the
// fluent-chain semantics it encodes differ.
//
// No exit sentinel
// ----------------
// Unlike LangGraph (`END`) / pydantic-graph (`End`) / LlamaIndex (`StopEvent`),
// a Mastra workflow has no in-graph terminal sentinel — the chain simply ends
// at `.commit()`. The shim invents no sentinel and never sets has_exit_branch;
// cycle bounding relies on shingan's structural cycle-exit
// (domain/rules/cycle.go `cycleHasExit`), which downgrades a cycle to Warning
// when some in-cycle node has an edge leaving the cycle (e.g. the post-loop
// continuation after a `.dountil`). This mirrors AutoGen, not the
// END-sentinel frameworks.
//
// Resource ownership
// ------------------
// `MastraParser` owns one subprocess worker (the same PythonWorker core, run
// with the Node binary — see subprocessWorker). Callers are expected to invoke
// `Close()` when done.
package parser

import (
	"fmt"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// mastraShimFilename is the runnable Node shim (source that imports the
// TypeScript Compiler API; its `typescript` dep is provided by
// ensureLangGraphJSDeps — the same bootstrap the LangGraph.js parser uses).
const mastraShimFilename = "export_mastra_server.mjs"

// mastraMissingNodeHint is appended to the worker's "not found in PATH" spawn
// error so users get a Node-flavoured install hint.
const mastraMissingNodeHint = "install Node.js 18+ to use the Mastra parser"

// MastraParser converts Mastra TypeScript/JavaScript workflow source into a
// Shingan WorkflowGraph by delegating to a long-lived Node worker. The worker
// is the same subprocess core that LangGraphJSParser uses; only the binary
// ("node") and the shim ("export_mastra_server.mjs") differ (per ADR-015).
type MastraParser struct {
	worker *PythonWorker

	mu       sync.Mutex
	healthOK bool
	healthCk bool
}

// MastraOption configures a MastraParser at construction time.
type MastraOption func(*mastraConfig)

type mastraConfig struct {
	scriptPath     string
	nodeBin        string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
}

// WithMastraScriptPath overrides the path to the shim .mjs script.
// Default: result of LocateShimNamed(export_mastra_server.mjs).
func WithMastraScriptPath(path string) MastraOption {
	return func(c *mastraConfig) { c.scriptPath = path }
}

// WithMastraNodeBinary overrides the Node interpreter used for the underlying
// worker. Default: "node".
func WithMastraNodeBinary(bin string) MastraOption {
	return func(c *mastraConfig) { c.nodeBin = bin }
}

// WithMastraWorker reuses a pre-constructed worker (for tests).
func WithMastraWorker(w *PythonWorker) MastraOption {
	return func(c *mastraConfig) { c.existingWorker = w }
}

// NewMastraParser instantiates the parser and (unless WithMastraWorker is
// supplied) spawns the underlying Node subprocess. The returned parser must be
// `Close()`d to release process resources.
func NewMastraParser(opts ...MastraOption) (*MastraParser, error) {
	cfg := &mastraConfig{
		nodeBin: "node",
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.existingWorker != nil {
		return &MastraParser{worker: cfg.existingWorker}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShimNamed(mastraShimFilename)
		if err != nil {
			return nil, fmt.Errorf("mastra parser: %w", err)
		}
	}

	// The shim imports the TypeScript compiler at runtime; make sure it is
	// installed next to the shim (a no-op once node_modules exists). This is
	// the SAME `typescript` bootstrap the LangGraph.js parser uses — both shims
	// share the one embedded shims/package.json, so there is no second install.
	if err := ensureLangGraphJSDeps(scriptPath); err != nil {
		return nil, err
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.nodeBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.nodeBin))
	}
	workerOpts = append(workerOpts, WithMissingBinaryHint(mastraMissingNodeHint))
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("mastra parser: %w", err)
	}
	return &MastraParser{worker: worker}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *MastraParser) SupportedFormat() string { return "mastra" }

// Parse converts inline TypeScript/JavaScript source into a WorkflowGraph by
// sending it to the worker via `parse_content`. The synthetic filename
// "<inline.ts>" is used because callers of this entry point do not have a real
// path on disk.
func (p *MastraParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.ts>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to the
// Node worker. The shim uses the extension to choose TS vs TSX parsing and
// records it as the node SourcePos.File.
func (p *MastraParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = "<inline.ts>"
	}
	raw, err := p.worker.Call("parse_content", map[string]string{
		"content":  string(input),
		"filename": filename,
	})
	if err != nil {
		return nil, fmt.Errorf("mastra parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to read the file from disk and export its Mastra
// workflow definition into Shingan's WorkflowGraph JSON shape. Implements the
// fileParser interface so the CLI directory walk reads .ts files directly.
func (p *MastraParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	raw, err := p.worker.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("mastra parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// Close releases the underlying Node worker.
func (p *MastraParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying worker has been shut down or killed
// (e.g. by a Call() timeout).
func (p *MastraParser) Closed() bool {
	if p == nil || p.worker == nil {
		return true
	}
	return p.worker.Closed()
}

// ensureHealthy lazily runs a health_check on first use. The check is memoised
// so failing fast on the same parser is the desired behaviour. The Node shim
// reports status "ok" whenever it loads (it is AST-only and imports no
// framework), so this gate effectively only fails when `node` itself is broken.
func (p *MastraParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errMastraUnhealthy
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("mastra parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errMastraUnhealthy
	}
	p.healthOK = true
	return nil
}

// errMastraUnhealthy is surfaced when the Node worker is reachable but reports
// a non-"ok" health status (e.g. the bundled parser failed to load). It wraps
// ErrPythonFrameworkMissing for symmetry with the other parsers so directory
// walks treat it as a global (not per-file) failure. The shim never imports
// @mastra/core, so in practice this only fires on a broken Node.
var errMastraUnhealthy = fmt.Errorf(
	"mastra parser: Node.js 18+ required for Mastra format (TypeScript shim failed to load): %w",
	ErrPythonFrameworkMissing,
)
