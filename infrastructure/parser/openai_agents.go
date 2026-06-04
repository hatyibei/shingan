// Package parser provides WorkflowParser implementations for different input formats.
//
// openai_agents.go: parser that ferries OpenAI Agents SDK (`@openai/agents`)
// multi-agent definitions (the `new Agent({...})` / `Agent.create({...})`
// constructor + `handoffs: [...]` API) across the Go ⇄ Node boundary via a
// long-lived JSON-RPC worker (`shims/export_openai_agents_server.mjs`).
//
// Onion layer: infrastructure. The Go side knows nothing about TypeScript AST
// or OpenAI-Agents internals — every framework-specific concern lives in the
// bundled Node shim. See ADR-015 (framework-by-framework PoC parsers) and
// ADR-009 for the long-lived-worker / degraded-mode pattern.
//
// The Node shim is AST-only: it parses .ts/.js via the TypeScript Compiler API
// and never imports @openai/agents, so the worker is healthy whenever Node +
// the bundled parser load. Like the LangGraph.js / Mastra parsers there is no
// "framework not installed" runtime gate — `node` alone is sufficient.
//
// Reuse
// -----
// This parser deliberately reuses the LangGraph.js / Mastra Node infrastructure
// verbatim: the same subprocess worker (run with the Node binary), the same
// `ensureLangGraphJSDeps` TypeScript bootstrap (the `typescript` dependency is
// shared — there is ONE shims/package.json), and the same `decodeShimGraph`
// wire decode. Only the shim script (`export_openai_agents_server.mjs`) and the
// handoff-graph semantics it encodes differ.
//
// Edges = handoffs
// ----------------
// An `Agent`'s `handoffs: [b, handoff(c)]` declares directed edges agentA->B,
// agentA->C. Each handoff target is resolved to a DECLARED `new Agent` /
// `Agent.create` binding in the same file; targets that don't resolve (imported
// from another module, computed) are OMITTED, never fabricated
// (dest-must-be-declared, mirroring the LangGraph.js shim). Agents-as-tools
// (`tools: [child.asTool()]`) are modelled as edges when the receiver resolves
// to a declared agent.
//
// No exit sentinel
// ----------------
// Unlike LangGraph (`END`) / pydantic-graph (`End`) / LlamaIndex (`StopEvent`),
// OpenAI Agents has no in-graph terminal sentinel — a run ends implicitly (final
// output / MaxTurnsExceededError / guardrails). The shim invents no sentinel and
// never sets has_exit_branch; cycle bounding relies on shingan's structural
// cycle-exit (domain/rules/cycle.go `cycleHasExit`), which downgrades a handoff
// cycle to Warning when some in-cycle agent has a handoff leaving the cycle, and
// keeps a pure handoff loop Critical. This mirrors AutoGen / Mastra, not the
// END-sentinel frameworks.
//
// Resource ownership
// ------------------
// `OpenAIAgentsParser` owns one subprocess worker (the same PythonWorker core,
// run with the Node binary — see subprocessWorker). Callers are expected to
// invoke `Close()` when done.
package parser

import (
	"fmt"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// openAIAgentsShimFilename is the runnable Node shim (source that imports the
// TypeScript Compiler API; its `typescript` dep is provided by
// ensureLangGraphJSDeps — the same bootstrap the LangGraph.js / Mastra parsers
// use).
const openAIAgentsShimFilename = "export_openai_agents_server.mjs"

// openAIAgentsMissingNodeHint is appended to the worker's "not found in PATH"
// spawn error so users get a Node-flavoured install hint.
const openAIAgentsMissingNodeHint = "install Node.js 18+ to use the OpenAI Agents parser"

// OpenAIAgentsParser converts OpenAI Agents SDK TypeScript/JavaScript source
// into a Shingan WorkflowGraph by delegating to a long-lived Node worker. The
// worker is the same subprocess core that LangGraphJSParser / MastraParser use;
// only the binary ("node") and the shim ("export_openai_agents_server.mjs")
// differ (per ADR-015).
type OpenAIAgentsParser struct {
	worker *PythonWorker

	mu       sync.Mutex
	healthOK bool
	healthCk bool
}

// OpenAIAgentsOption configures an OpenAIAgentsParser at construction time.
type OpenAIAgentsOption func(*openAIAgentsConfig)

type openAIAgentsConfig struct {
	scriptPath     string
	nodeBin        string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
}

// WithOpenAIAgentsScriptPath overrides the path to the shim .mjs script.
// Default: result of LocateShimNamed(export_openai_agents_server.mjs).
func WithOpenAIAgentsScriptPath(path string) OpenAIAgentsOption {
	return func(c *openAIAgentsConfig) { c.scriptPath = path }
}

// WithOpenAIAgentsNodeBinary overrides the Node interpreter used for the
// underlying worker. Default: "node".
func WithOpenAIAgentsNodeBinary(bin string) OpenAIAgentsOption {
	return func(c *openAIAgentsConfig) { c.nodeBin = bin }
}

// WithOpenAIAgentsWorker reuses a pre-constructed worker (for tests).
func WithOpenAIAgentsWorker(w *PythonWorker) OpenAIAgentsOption {
	return func(c *openAIAgentsConfig) { c.existingWorker = w }
}

// NewOpenAIAgentsParser instantiates the parser and (unless
// WithOpenAIAgentsWorker is supplied) spawns the underlying Node subprocess. The
// returned parser must be `Close()`d to release process resources.
func NewOpenAIAgentsParser(opts ...OpenAIAgentsOption) (*OpenAIAgentsParser, error) {
	cfg := &openAIAgentsConfig{
		nodeBin: "node",
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.existingWorker != nil {
		return &OpenAIAgentsParser{worker: cfg.existingWorker}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShimNamed(openAIAgentsShimFilename)
		if err != nil {
			return nil, fmt.Errorf("openai-agents parser: %w", err)
		}
	}

	// The shim imports the TypeScript compiler at runtime; make sure it is
	// installed next to the shim (a no-op once node_modules exists). This is
	// the SAME `typescript` bootstrap the LangGraph.js / Mastra parsers use —
	// all three shims share the one embedded shims/package.json, so there is no
	// extra install.
	if err := ensureLangGraphJSDeps(scriptPath); err != nil {
		return nil, err
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.nodeBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.nodeBin))
	}
	workerOpts = append(workerOpts, WithMissingBinaryHint(openAIAgentsMissingNodeHint))
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("openai-agents parser: %w", err)
	}
	return &OpenAIAgentsParser{worker: worker}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *OpenAIAgentsParser) SupportedFormat() string { return "openai-agents" }

// Parse converts inline TypeScript/JavaScript source into a WorkflowGraph by
// sending it to the worker via `parse_content`. The synthetic filename
// "<inline.ts>" is used because callers of this entry point do not have a real
// path on disk.
func (p *OpenAIAgentsParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.ts>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to the
// Node worker. The shim uses the extension to choose TS vs TSX parsing and
// records it as the node SourcePos.File.
func (p *OpenAIAgentsParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
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
		return nil, fmt.Errorf("openai-agents parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to read the file from disk and export its OpenAI
// Agents definition into Shingan's WorkflowGraph JSON shape. Implements the
// fileParser interface so the CLI directory walk reads .ts files directly.
func (p *OpenAIAgentsParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	raw, err := p.worker.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("openai-agents parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// Close releases the underlying Node worker.
func (p *OpenAIAgentsParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying worker has been shut down or killed
// (e.g. by a Call() timeout).
func (p *OpenAIAgentsParser) Closed() bool {
	if p == nil || p.worker == nil {
		return true
	}
	return p.worker.Closed()
}

// ensureHealthy lazily runs a health_check on first use. The check is memoised
// so failing fast on the same parser is the desired behaviour. The Node shim
// reports status "ok" whenever it loads (it is AST-only and imports no
// framework), so this gate effectively only fails when `node` itself is broken.
func (p *OpenAIAgentsParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errOpenAIAgentsUnhealthy
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("openai-agents parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errOpenAIAgentsUnhealthy
	}
	p.healthOK = true
	return nil
}

// errOpenAIAgentsUnhealthy is surfaced when the Node worker is reachable but
// reports a non-"ok" health status (e.g. the bundled parser failed to load). It
// wraps ErrPythonFrameworkMissing for symmetry with the other parsers so
// directory walks treat it as a global (not per-file) failure. The shim never
// imports @openai/agents, so in practice this only fires on a broken Node.
var errOpenAIAgentsUnhealthy = fmt.Errorf(
	"openai-agents parser: Node.js 18+ required for OpenAI Agents format (TypeScript shim failed to load): %w",
	ErrPythonFrameworkMissing,
)
