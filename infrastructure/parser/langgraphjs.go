// Package parser provides WorkflowParser implementations for different input formats.
//
// langgraphjs.go: parser that ferries LangGraph.js (TypeScript / JavaScript,
// `@langchain/langgraph`) StateGraph definitions across the Go ⇄ Node boundary
// via a long-lived JSON-RPC worker (`shims/export_langgraphjs_server.mjs`).
//
// Onion layer: infrastructure. The Go side knows nothing about TypeScript AST
// or langgraph.js internals — every framework-specific concern lives in the
// bundled Node shim. See ADR-015 (LangGraph.js PoC parser) and ADR-009 for the
// long-lived-worker / degraded-mode pattern.
//
// The Node shim is AST-only: it parses .ts/.js via the TypeScript Compiler API
// and never imports @langchain/langgraph, so the worker is healthy whenever
// Node + the bundled parser load. Unlike the Python parsers there is no
// "framework not installed" runtime gate — `node` alone is sufficient.
//
// Resource ownership
// ------------------
// `LangGraphJSParser` owns one subprocess worker (the same PythonWorker core,
// run with the Node binary — see subprocessWorker). Callers are expected to
// invoke `Close()` when done.
package parser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hatyibei/shingan/domain"
)

// langGraphJSShimFilename is the runnable Node shim (source that imports the
// TypeScript Compiler API; its `typescript` dep is provided by
// ensureLangGraphJSDeps).
const langGraphJSShimFilename = "export_langgraphjs_server.mjs"

// langGraphJSMissingNodeHint is appended to the worker's "not found in PATH"
// spawn error so users get a Node-flavoured install hint.
const langGraphJSMissingNodeHint = "install Node.js 18+ to use the LangGraph.js parser"

// LangGraphJSParser converts LangGraph.js TypeScript/JavaScript source into a
// Shingan WorkflowGraph by delegating to a long-lived Node worker. The worker
// is the same subprocess core that LangGraphParser / CrewAIParser use; only
// the binary ("node") and the shim ("export_langgraphjs_server.mjs") differ
// (per ADR-015).
type LangGraphJSParser struct {
	worker *PythonWorker

	mu       sync.Mutex
	healthOK bool
	healthCk bool
}

// LangGraphJSOption configures a LangGraphJSParser at construction time.
type LangGraphJSOption func(*langGraphJSConfig)

type langGraphJSConfig struct {
	scriptPath     string
	nodeBin        string
	workerOpts     []PythonWorkerOption
	existingWorker *PythonWorker
}

// WithLangGraphJSScriptPath overrides the path to the shim .mjs script.
// Default: result of LocateShimNamed(export_langgraphjs_server.mjs).
func WithLangGraphJSScriptPath(path string) LangGraphJSOption {
	return func(c *langGraphJSConfig) { c.scriptPath = path }
}

// WithLangGraphJSNodeBinary overrides the Node interpreter used for the
// underlying worker. Default: "node".
func WithLangGraphJSNodeBinary(bin string) LangGraphJSOption {
	return func(c *langGraphJSConfig) { c.nodeBin = bin }
}

// WithLangGraphJSWorker reuses a pre-constructed worker (for tests).
func WithLangGraphJSWorker(w *PythonWorker) LangGraphJSOption {
	return func(c *langGraphJSConfig) { c.existingWorker = w }
}

// NewLangGraphJSParser instantiates the parser and (unless
// WithLangGraphJSWorker is supplied) spawns the underlying Node subprocess.
// The returned parser must be `Close()`d to release process resources.
func NewLangGraphJSParser(opts ...LangGraphJSOption) (*LangGraphJSParser, error) {
	cfg := &langGraphJSConfig{
		nodeBin: "node",
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.existingWorker != nil {
		return &LangGraphJSParser{worker: cfg.existingWorker}, nil
	}

	scriptPath := cfg.scriptPath
	if scriptPath == "" {
		var err error
		scriptPath, err = LocateShimNamed(langGraphJSShimFilename)
		if err != nil {
			return nil, fmt.Errorf("langgraph-js parser: %w", err)
		}
	}

	// The shim imports the TypeScript compiler at runtime; make sure it is
	// installed next to the shim (a no-op once node_modules exists). This is
	// the "runtime npm install" distribution model (ADR-015): we ship the
	// ~21KB source shim, not a ~10MB bundled compiler, so the binary stays
	// small at the cost of a one-time `npm install` on first use.
	if err := ensureLangGraphJSDeps(scriptPath); err != nil {
		return nil, err
	}

	workerOpts := append([]PythonWorkerOption{}, cfg.workerOpts...)
	if cfg.nodeBin != "" {
		workerOpts = append(workerOpts, WithPythonBinary(cfg.nodeBin))
	}
	workerOpts = append(workerOpts, WithMissingBinaryHint(langGraphJSMissingNodeHint))
	worker, err := NewPythonWorker(scriptPath, workerOpts...)
	if err != nil {
		return nil, fmt.Errorf("langgraph-js parser: %w", err)
	}
	return &LangGraphJSParser{worker: worker}, nil
}

// SupportedFormat implements application.WorkflowParser.
func (p *LangGraphJSParser) SupportedFormat() string { return "langgraph-js" }

// Parse converts inline TypeScript/JavaScript source into a WorkflowGraph by
// sending it to the worker via `parse_content`. The synthetic filename
// "<inline.ts>" is used because callers of this entry point do not have a real
// path on disk.
func (p *LangGraphJSParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.ParseWithFilename(input, "<inline.ts>")
}

// ParseWithFilename is Parse but with an explicit filename hint passed to the
// Node worker. The shim uses the extension to choose TS vs TSX parsing and
// records it as the node SourcePos.File.
func (p *LangGraphJSParser) ParseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
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
		return nil, fmt.Errorf("langgraph-js parser: parse_content: %w", err)
	}
	return decodeShimGraph(raw)
}

// ParseFile asks the worker to read the file from disk and export its
// StateGraph definition into Shingan's WorkflowGraph JSON shape. Implements
// the fileParser interface so the CLI directory walk reads .ts files directly.
func (p *LangGraphJSParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if err := p.ensureHealthy(); err != nil {
		return nil, err
	}
	raw, err := p.worker.Call("parse_file", map[string]string{"path": path})
	if err != nil {
		return nil, fmt.Errorf("langgraph-js parser: parse_file %q: %w", path, err)
	}
	return decodeShimGraph(raw)
}

// Close releases the underlying Node worker.
func (p *LangGraphJSParser) Close() error {
	if p == nil || p.worker == nil {
		return nil
	}
	return p.worker.Close()
}

// Closed reports whether the underlying worker has been shut down or killed
// (e.g. by a Call() timeout).
func (p *LangGraphJSParser) Closed() bool {
	if p == nil || p.worker == nil {
		return true
	}
	return p.worker.Closed()
}

// ensureHealthy lazily runs a health_check on first use. The check is memoised
// so failing fast on the same parser is the desired behaviour. The Node shim
// reports status "ok" whenever it loads (it is AST-only and imports no
// framework), so this gate effectively only fails when `node` itself is broken.
func (p *LangGraphJSParser) ensureHealthy() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCk {
		if p.healthOK {
			return nil
		}
		return errLangGraphJSUnhealthy
	}
	p.healthCk = true
	hc, err := p.worker.HealthCheck()
	if err != nil {
		p.healthOK = false
		return fmt.Errorf("langgraph-js parser: health check: %w", err)
	}
	if hc.Status != "ok" {
		p.healthOK = false
		return errLangGraphJSUnhealthy
	}
	p.healthOK = true
	return nil
}

// errLangGraphJSUnhealthy is surfaced when the Node worker is reachable but
// reports a non-"ok" health status (e.g. the bundled parser failed to load).
// It wraps ErrPythonFrameworkMissing for symmetry with the Python parsers so
// directory walks treat it as a global (not per-file) failure.
var errLangGraphJSUnhealthy = fmt.Errorf(
	"langgraph-js parser: Node.js 18+ required for LangGraph.js format (TypeScript shim failed to load): %w",
	ErrPythonFrameworkMissing,
)

// langGraphJSDepsTimeout bounds the one-time `npm install` of the TypeScript
// compiler. A cold install pulls ~1 package over the network; 120s is generous.
const langGraphJSDepsTimeout = 120 * time.Second

// ensureLangGraphJSDeps guarantees the shim's sole runtime dependency
// (the `typescript` package) is installed next to the shim before the Node
// worker spawns. It is a no-op once `node_modules/typescript` exists, so the
// cost is paid only on first use (or after a shingan upgrade extracts a fresh
// shim). In the embedded/npm-distribution case the shim is extracted to a
// cache dir without a package.json, so we materialise the embedded one there
// before installing.
func ensureLangGraphJSDeps(shimPath string) error {
	dir := filepath.Dir(shimPath)

	// Fast path: typescript already present (dev checkout, or a warm cache).
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "typescript", "package.json")); err == nil {
		return nil
	}

	// The extracted-shim (npm-dist) cache dir has no package.json — write the
	// embedded manifest so `npm install` has something to resolve.
	pkgPath := filepath.Join(dir, "package.json")
	if _, err := os.Stat(pkgPath); err != nil {
		data, rerr := fs.ReadFile(shimsFS, "shims/package.json")
		if rerr != nil {
			return fmt.Errorf("langgraph-js parser: embedded package.json not bundled: %w", rerr)
		}
		if werr := os.WriteFile(pkgPath, data, 0o644); werr != nil {
			return fmt.Errorf("langgraph-js parser: write package.json to %q: %w", dir, werr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), langGraphJSDepsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "install", "--omit=dev", "--no-audit", "--no-fund", "--loglevel=error")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") {
			return fmt.Errorf("langgraph-js parser: `npm` not found in PATH; install Node.js 18+ (with npm) to use the LangGraph.js format: %w: %w", err, ErrPythonFrameworkMissing)
		}
		return fmt.Errorf("langgraph-js parser: `npm install` for the TypeScript shim failed in %q: %v\n%s: %w", dir, err, strings.TrimSpace(string(out)), ErrPythonFrameworkMissing)
	}
	return nil
}
