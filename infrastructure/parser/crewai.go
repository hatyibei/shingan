// Package parser provides WorkflowParser implementations for different input formats.
//
// crewai.go: parser that ferries Python CrewAI Crew/Agent/Task definitions
// across the Go ⇄ Python boundary via a long-lived JSON-RPC worker
// (`scripts/export_crewai_server.py`).
//
// Onion layer: infrastructure. The Go side knows nothing about Python AST or
// crewai internals — every framework-specific concern lives in the shim.
// See ADR-013 for the rationale (CrewAI parser strategy reusing LangGraph
// PythonWorker) and ADR-009 for the long-lived-worker / degraded-mode pattern.
//
// Resource ownership
// ------------------
// `CrewAIParser` owns one `PythonWorker`. Callers are expected to invoke
// `Close()` when done; tests/CLIs short-circuit by deferring it. ParserFactory
// stores a single instance per analysis run, matching the v0.6 LSP design.
package parser

import (
	"fmt"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// CrewAIParser converts CrewAI Python source into a Shingan WorkflowGraph
// by delegating to a long-lived Python worker. The worker is the same
// PythonWorker implementation that LangGraphParser uses; only the shim
// script differs (per ADR-013).
type CrewAIParser struct {
	worker *PythonWorker

	mu       sync.Mutex
	healthOK bool
	healthCk bool
}

// CrewAIOption configures a CrewAIParser at construction time.
type CrewAIOption func(*crewAIConfig)

type crewAIConfig struct {
	scriptPath     string
	pythonBin      string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
}

// WithCrewAIScriptPath overrides the path to the shim Python script.
// Default: locates `scripts/export_crewai_server.py` next to the binary.
func WithCrewAIScriptPath(path string) CrewAIOption {
	return func(c *crewAIConfig) { c.scriptPath = path }
}

// WithCrewAIPythonBinary overrides the Python interpreter used for the
// underlying worker. Default: "python3".
func WithCrewAIPythonBinary(bin string) CrewAIOption {
	return func(c *crewAIConfig) { c.pythonBin = bin }
}

// WithCrewAIWorker reuses a pre-constructed PythonWorker (for tests).
func WithCrewAIWorker(w *PythonWorker) CrewAIOption {
	return func(c *crewAIConfig) { c.existingWorker = w }
}

// NewCrewAIParser instantiates the parser and (unless WithCrewAIWorker is
// supplied) spawns the underlying Python subprocess. The returned parser
// must be `Close()`d to release process resources.
func NewCrewAIParser(opts ...CrewAIOption) (*CrewAIParser, error) {
	cfg := &crewAIConfig{
		pythonBin: "python3",
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.existingWorker != nil {
		return &CrewAIParser{worker: cfg.existingWorker}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShimNamed("export_crewai_server.py")
		if err != nil {
			return nil, fmt.Errorf("crewai parser: %w", err)
		}
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.pythonBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.pythonBin))
	}
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("crewai parser: %w", err)
	}
	return &CrewAIParser{worker: worker}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *CrewAIParser) SupportedFormat() string { return "crewai" }

// Parse converts inline Python source into a WorkflowGraph by sending it to
// the worker via `parse_content`. The synthetic filename "<inline.py>" is
// used because callers of this entry point do not have a real path on disk.
func (p *CrewAIParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.py>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to
// the Python worker. The shim sets `module.__file__ = filename` and inserts
// filename's parent directory at the head of sys.path before executing,
// so workflows split across sibling modules resolve correctly.
func (p *CrewAIParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
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
		return nil, fmt.Errorf("crewai parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to import the file from disk and export its
// Crew definition into Shingan's WorkflowGraph JSON shape.
func (p *CrewAIParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	raw, err := p.worker.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("crewai parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// Close releases the underlying Python worker.
func (p *CrewAIParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying Python worker has been shut down
// or killed (e.g. by a Call() timeout).
func (p *CrewAIParser) Closed() bool {
	if p == nil || p.worker == nil {
		return true
	}
	return p.worker.Closed()
}

// ensureHealthy lazily runs a health_check on first use. The check is
// memoised so failing fast on the same parser is the desired behaviour.
func (p *CrewAIParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errCrewAIMissing
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("crewai parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errCrewAIMissing
	}
	p.healthOK = true
	return nil
}

// errCrewAIMissing is the canonical error surfaced when the Python
// interpreter is reachable but `crewai` itself is not importable.
// Tests assert against this exact message; do not reword without bumping
// CHANGELOG. It wraps ErrPythonFrameworkMissing so directory walks can
// distinguish "framework missing" from per-file syntax errors via
// errors.Is and propagate the former (Codex iter4 P2).
var errCrewAIMissing = fmt.Errorf(
	"crewai parser: Python 3.x and `pip install crewai` (>=0.50.0) required for CrewAI format: %w",
	ErrPythonFrameworkMissing,
)
