// Package parser provides WorkflowParser implementations for different input formats.
//
// langgraph.go: parser that ferries Python LangGraph StateGraph definitions
// across the Go ⇄ Python boundary via a long-lived JSON-RPC worker
// (`scripts/export_langgraph_server.py`).
//
// Onion layer: infrastructure. The Go side knows nothing about Python AST or
// langgraph internals — every framework-specific concern lives in the shim.
// See ADR-011 for the rationale (LangGraph as Phase 1 primary parser) and
// ADR-009 for the long-lived-worker / degraded-mode pattern.
//
// Resource ownership
// ------------------
// `LangGraphParser` owns one `PythonWorker`. Callers are expected to invoke
// `Close()` when done; tests/CLIs short-circuit by deferring it. ParserFactory
// stores a single instance per analysis run, matching the v0.6 LSP design.
package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// LangGraphParser converts LangGraph Python source into a Shingan
// WorkflowGraph by delegating to a long-lived Python worker.
type LangGraphParser struct {
	worker *PythonWorker

	// scriptPath and workerOpts are retained so directory-mode parsing
	// (ParseFilesMulti) can spawn additional sibling workers for parallelism.
	// They are empty when the parser was constructed with an existing worker
	// (WithLangGraphWorker), in which case the pool degrades to the single
	// worker.
	scriptPath string
	workerOpts []PythonWorkerOption
	workers    int // directory-mode pool size (>=1)

	mu       sync.Mutex
	healthOK bool
	healthCk bool // whether HealthCheck has been called yet
}

// defaultLangGraphWorkers is the directory-mode pool size when unset. Two
// balances throughput against the per-worker Python startup + memory cost.
const defaultLangGraphWorkers = 2

// LangGraphOption configures a LangGraphParser at construction time.
type LangGraphOption func(*langGraphConfig)

type langGraphConfig struct {
	scriptPath     string
	pythonBin      string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
	workers        int
}

// WithLangGraphScriptPath overrides the path to the shim Python script.
// Default: result of LocateShim().
func WithLangGraphScriptPath(path string) LangGraphOption {
	return func(c *langGraphConfig) { c.scriptPath = path }
}

// WithLangGraphPythonBinary overrides the Python interpreter used for the
// underlying worker. Default: "python3".
func WithLangGraphPythonBinary(bin string) LangGraphOption {
	return func(c *langGraphConfig) { c.pythonBin = bin }
}

// WithLangGraphWorker reuses a pre-constructed PythonWorker (for tests).
func WithLangGraphWorker(w *PythonWorker) LangGraphOption {
	return func(c *langGraphConfig) { c.existingWorker = w }
}

// WithLangGraphWorkers sets the directory-mode worker-pool size used by
// ParseFilesMulti. Values < 1 are clamped to 1 (serial). Default is 2.
// Single-file Parse / ParseFile always use the one primary worker regardless.
func WithLangGraphWorkers(n int) LangGraphOption {
	return func(c *langGraphConfig) { c.workers = n }
}

// NewLangGraphParser instantiates the parser and (unless WithLangGraphWorker
// is supplied) spawns the underlying Python subprocess. The returned parser
// must be `Close()`d to release process resources.
func NewLangGraphParser(opts ...LangGraphOption) (*LangGraphParser, error) {
	cfg := &langGraphConfig{
		pythonBin: "python3",
		workers:   defaultLangGraphWorkers,
	}
	for _, o := range opts {
		o(cfg)
	}
	workers := cfg.workers
	if workers < 1 {
		workers = 1
	}

	if cfg.existingWorker != nil {
		// No scriptPath/workerOpts retained: the pool degrades to this one
		// worker (sibling spawning needs a script path).
		return &LangGraphParser{worker: cfg.existingWorker, workers: workers}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShim()
		if err != nil {
			return nil, fmt.Errorf("langgraph parser: %w", err)
		}
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.pythonBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.pythonBin))
	}
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("langgraph parser: %w", err)
	}
	return &LangGraphParser{
		worker:     worker,
		scriptPath: scriptPath,
		workerOpts: workerOpts,
		workers:    workers,
	}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *LangGraphParser) SupportedFormat() string { return "langgraph" }

// Parse converts inline Python source into a WorkflowGraph by sending it to
// the worker via `parse_content`. The synthetic filename "<inline.py>" is
// used because callers of this entry point do not have a real path on disk
// — the worker logs it for diagnostics but cannot resolve sibling imports
// against it. Use ParseFile when the source is on disk, or
// ParseWithFilename when you want to pass an LSP buffer's on-disk URI as
// a filename hint for sys.path resolution.
func (p *LangGraphParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.py>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to
// the Python worker. The shim sets `module.__file__ = filename` and
// inserts filename's parent directory at the head of sys.path before
// executing the code, so workflows split across sibling modules
// (`from .helpers import ...`) resolve correctly.
//
// Per Codex iter3 P1 review: the LSP must thread the editor buffer's
// on-disk URI here so the same file analyzes identically through CLI
// (`ParseFile`) and LSP (this method on unsaved-buffer content).
func (p *LangGraphParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
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
		return nil, fmt.Errorf("langgraph parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to import the file from disk and export its
// StateGraph definition into Shingan's WorkflowGraph JSON shape.
func (p *LangGraphParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	return parseFileOn(p.worker, path)
}

// parseFileOn issues a parse_file RPC on a specific worker and decodes the
// result. Shared by ParseFile and the ParseFilesMulti pool so both paths agree
// on error wording and decoding.
func parseFileOn(w *PythonWorker, path string) (*domain.WorkflowGraph, error) {
	raw, err := w.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("langgraph parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// FileParseResult pairs a source path with its parse outcome, returned by
// ParseFilesMulti. Exactly one of Graph / Err is meaningful per entry.
type FileParseResult struct {
	Path  string
	Graph *domain.WorkflowGraph
	Err   error
}

// ParseFilesMulti parses many files concurrently using a pool of up to
// `workers` Python workers (the primary plus freshly-spawned siblings), and
// returns one FileParseResult per input path. Results are sorted by Path so the
// output is deterministic regardless of completion order.
//
// Framework health is gated once on the primary worker: if langgraph isn't
// importable the whole batch fails with that error (callers propagate it as a
// global failure rather than a per-file skip). Per-file parse errors are
// reported in the corresponding FileParseResult.Err.
//
// The pool degrades gracefully: if sibling workers can't be spawned (no script
// path, or a spawn error) it uses however many it has, down to the single
// primary worker (serial).
func (p *LangGraphParser) ParseFilesMulti(paths []string) ([]FileParseResult, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}

	n := p.workers
	if n < 1 {
		n = 1
	}
	if n > len(paths) {
		n = len(paths)
	}

	pool := []*PythonWorker{p.worker}
	var siblings []*PythonWorker
	if p.scriptPath != "" {
		for len(pool) < n {
			w, err := NewPythonWorker(p.scriptPath, p.workerOpts...)
			if err != nil {
				break // fall back to the workers we already have
			}
			siblings = append(siblings, w)
			pool = append(pool, w)
		}
	}
	defer func() {
		for _, w := range siblings {
			_ = w.Close()
		}
	}()

	jobs := make(chan string)
	results := make(chan FileParseResult)
	var wg sync.WaitGroup
	for _, w := range pool {
		wg.Add(1)
		go func(w *PythonWorker) {
			defer wg.Done()
			for path := range jobs {
				g, err := parseFileOn(w, path)
				results <- FileParseResult{Path: path, Graph: g, Err: err}
			}
		}(w)
	}
	go func() {
		for _, path := range paths {
			jobs <- path
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]FileParseResult, 0, len(paths))
	for r := range results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Close releases the underlying Python worker.
func (p *LangGraphParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying Python worker has been shut down
// or killed (e.g. by a Call() timeout). The LSP server uses this to decide
// whether the cached parser is still usable; a dead worker must be
// replaced with a fresh subprocess (Codex iter4 P1).
func (p *LangGraphParser) Closed() bool {
	if p == nil || p.worker == nil {
		return true
	}
	return p.worker.Closed()
}

// ensureHealthy lazily runs a health_check on first use. The check is
// memoised because failing fast on the same parser is the desired behaviour
// (the user will see a clear actionable error and either install langgraph or
// pick a different format).
func (p *LangGraphParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errLangGraphMissing
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("langgraph parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errLangGraphMissing
	}
	p.healthOK = true
	return nil
}

// errLangGraphMissing is the canonical error surfaced when the Python
// interpreter is reachable but `langgraph` itself is not importable.
// Tests assert against this exact message; do not reword without bumping
// CHANGELOG. It wraps ErrPythonFrameworkMissing so directory walks can
// distinguish "framework missing" from per-file syntax errors via
// errors.Is and propagate the former (Codex iter4 P2).
var errLangGraphMissing = fmt.Errorf(
	"langgraph parser: Python 3.x and `pip install langgraph` required for LangGraph format: %w",
	ErrPythonFrameworkMissing,
)

// shimGraphMetadata mirrors the optional metadata block emitted by the shim.
// We accept and ignore unknown keys so future shim versions can add fields
// without breaking the Go side.
type shimGraphMetadata struct {
	SourceFormat          string `json:"source_format,omitempty"`
	SourceFile            string `json:"source_file,omitempty"`
	LangGraphVersion      string `json:"langgraph_version,omitempty"`
	ConditionalEdgeReason string `json:"conditional_edge_reason,omitempty"`
}

// shimGraph is the on-wire shape produced by `_build_graph` in the shim.
// The `metadata` block is informative — Shingan rules don't need it yet but
// surfacing it through the parser keeps it available for Track R follow-up.
type shimGraph struct {
	Nodes       json.RawMessage `json:"nodes"`
	Edges       []domain.Edge   `json:"edges"`
	EntryNodeID string          `json:"entry_node_id"`
	// EntryAmbiguous lets a shim signal that the source has multiple
	// plausible roots and none is statically the canonical entry (e.g. a
	// pydantic-graph with several zero-in-degree BaseNode classes and no
	// explicit graph.run(Start())). When true the shim leaves
	// entry_node_id empty and reachability skips the graph rather than
	// reporting the non-chosen roots as unreachable. Absent for shims that
	// always resolve an entry — defaults to false, so existing parsers are
	// unaffected.
	EntryAmbiguous bool              `json:"entry_ambiguous,omitempty"`
	Metadata       shimGraphMetadata `json:"metadata"`
}

// shimNode mirrors a single node entry. We keep `pos` as the canonical struct
// from the domain layer so SourcePos handling stays consistent across parsers.
type shimNode struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          domain.NodeType   `json:"type"`
	Config        map[string]any    `json:"config"`
	Pos           *domain.SourcePos `json:"pos"`
	HasExitBranch bool              `json:"has_exit_branch"`
}

// decodeShimGraph converts the raw JSON RPC result from the shim into a
// domain.WorkflowGraph. We do not use WorkflowGraph.UnmarshalJSON directly
// because the shim emits `pos` even when empty (Python objects have no
// elision rules), and `domain.SourcePos` already supports zero values.
func decodeShimGraph(raw json.RawMessage) (*domain.WorkflowGraph, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("langgraph parser: empty response")
	}
	var sg shimGraph
	if err := json.Unmarshal(raw, &sg); err != nil {
		return nil, fmt.Errorf("langgraph parser: decode result: %w", err)
	}
	var rawNodes []shimNode
	if len(sg.Nodes) > 0 {
		if err := json.Unmarshal(sg.Nodes, &rawNodes); err != nil {
			return nil, fmt.Errorf("langgraph parser: decode nodes: %w", err)
		}
	}

	graph := &domain.WorkflowGraph{
		Nodes:          make(map[string]*domain.Node, len(rawNodes)),
		Edges:          sg.Edges,
		EntryNodeID:    sg.EntryNodeID,
		EntryAmbiguous: sg.EntryAmbiguous,
	}
	for _, n := range rawNodes {
		dn := &domain.Node{
			ID:            n.ID,
			Name:          n.Name,
			Type:          n.Type,
			Config:        n.Config,
			HasExitBranch: n.HasExitBranch,
		}
		if n.Pos != nil {
			dn.Pos = *n.Pos
		}
		graph.Nodes[n.ID] = dn
	}
	return graph, nil
}
