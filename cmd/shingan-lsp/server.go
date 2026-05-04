package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/cache"
	"github.com/hatyibei/shingan/infrastructure/factory"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// docState holds the most recent textual snapshot of a document together
// with the cache key used to memoise its analysis. We retain the raw text
// (rather than recomputing it from incremental ContentChanges) so the
// rest of the server can stay agnostic about the document sync mode — we
// always advertise SyncKindFull on initialize for that exact reason.
type docState struct {
	languageID string
	format     string
	content    string
	hashKey    cache.Key

	// graph and findings are kept side-by-side so Hover and CodeAction can
	// look up node metadata without re-running analysis. Both are nil
	// while the document has not yet been analyzed (e.g. the very first
	// didOpen before parse).
	graph    *domain.WorkflowGraph
	findings []domain.Finding
}

// publisher is the minimal slice of protocol.Client that the diagnostics
// path needs. We accept this interface (rather than the concrete client)
// so the server can be unit tested with a stub publisher that captures
// emitted diagnostics — see main_test.go.
type publisher interface {
	PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error
}

// Server is Shingan's LSP language server.
//
// Composition:
//   - baseServer covers ~60 LSP methods we don't implement (Completion,
//     Definition, etc.), keeping this file focused on what matters.
//   - parserFactory / analyzerFactory / orchestrator are the same building
//     blocks the CLI uses; we don't reimplement analysis logic for the LSP.
//   - cache is consulted on every didChange / didOpen so unchanged content
//     skips parse + analyze. This is ADR-009 layer 1.
//   - pythonHealth controls whether the server reports degraded mode;
//     today no rule actually requires Python, so degraded mode only
//     surfaces an informational diagnostic. Track P will tighten this.
type Server struct {
	baseServer

	publisher publisher

	parserFactory   *factory.ParserFactory
	analyzerFactory *factory.AnalyzerFactory
	orchestrator    *application.AnalysisOrchestrator

	cache        *cache.AnalysisCache
	pythonHealth *parser.PythonHealth

	// docMu protects docs. didChange and didOpen are concurrent writers in
	// principle (the LSP client may interleave them), and Hover / CodeAction
	// are concurrent readers — so a plain map would race.
	docMu sync.RWMutex
	docs  map[uri.URI]*docState

	// shutdownRequested is set by Shutdown() and observed by Exit() to
	// distinguish a graceful shutdown from an abnormal exit. Today both
	// paths simply tear down; we keep the flag for future cleanup hooks.
	shutdownRequested bool
}

// NewServer constructs a Server bound to the given client publisher. All
// dependencies have sensible defaults; no constructor flag exists today,
// matching the "drop-in stdio LSP" usage model.
func NewServer(p publisher) *Server {
	return &Server{
		publisher:       p,
		parserFactory:   factory.NewParserFactory(),
		analyzerFactory: factory.NewAnalyzerFactory(),
		orchestrator:    application.NewAnalysisOrchestrator(),
		cache:           cache.NewAnalysisCache(cache.DefaultSize),
		pythonHealth:    parser.NewPythonHealth(),
		docs:            map[uri.URI]*docState{},
	}
}

// PythonHealth exposes the embedded probe so cmd/shingan-lsp/main.go can
// kick off the initial check before we accept any client traffic. Tests
// also use this hook to inject a fake probe.
func (s *Server) PythonHealth() *parser.PythonHealth { return s.pythonHealth }

// SetCache replaces the analysis cache. Test-only knob; production callers
// rely on the default 512-entry / 1h-TTL cache wired by NewServer.
func (s *Server) SetCache(c *cache.AnalysisCache) { s.cache = c }

// --- Lifecycle methods -----------------------------------------------------

// Initialize advertises the small set of LSP capabilities we actually
// implement. SyncKindFull is intentional: parsing the agent JSON / Go code
// is cheap relative to maintaining an incremental document model in the
// LSP server. CodeAction and Hover are advertised so the editor's UI shows
// the right affordances even if our handlers currently return inert
// results for findings without source positions.
func (s *Server) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:   protocol.TextDocumentSyncKindFull,
			HoverProvider:      true,
			CodeActionProvider: true,
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    "shingan-lsp",
			Version: serverVersion,
		},
	}, nil
}

// Initialized fires once the client has finished its half of the handshake.
// We use this hook to perform the initial Python probe synchronously so the
// very first didOpen / didChange already sees the right Status — otherwise
// the probe would race the first analysis and silently swallow the
// degraded-mode diagnostic on cold start. The probe is inexpensive (a
// single `python3 --version` call with a 3 s timeout); blocking the
// Initialized handler for that duration is well within LSP latency
// expectations.
func (s *Server) Initialized(ctx context.Context, _ *protocol.InitializedParams) error {
	_, _ = s.pythonHealth.RunCheck(ctx)
	return nil
}

// Shutdown is called before Exit. We record intent so Exit can distinguish
// a graceful from an abrupt termination. The protocol itself does not
// require freeing resources here; the process exits shortly after.
func (s *Server) Shutdown(_ context.Context) error {
	s.shutdownRequested = true
	return nil
}

// --- Document lifecycle ----------------------------------------------------

// DidOpen analyzes the freshly-opened document. We mirror this through the
// same path didChange uses: cache lookup → parse + analyze on miss → emit
// diagnostics. Every didOpen MUST publish (even if the document has no
// findings) so editors immediately see "this language is alive" feedback.
func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	doc := params.TextDocument
	return s.analyzeAndPublish(ctx, doc.URI, string(doc.LanguageID), doc.Text, uint32(doc.Version))
}

// DidChange reanalyses on every full-document content change. Because we
// advertise SyncKindFull, the content of the last entry in ContentChanges
// is already the entire post-edit document — no patch reassembly needed.
func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// SyncKindFull guarantees a single change with the full text.
	text := params.ContentChanges[len(params.ContentChanges)-1].Text

	s.docMu.RLock()
	prev, ok := s.docs[params.TextDocument.URI]
	s.docMu.RUnlock()

	languageID := ""
	if ok {
		languageID = prev.languageID
	}

	return s.analyzeAndPublish(ctx, params.TextDocument.URI, languageID, text, uint32(params.TextDocument.Version))
}

// DidClose flushes the cached doc state and clears any lingering
// diagnostics in the editor. Without the explicit empty publish, VS Code
// keeps the last set of diagnostics in the Problems panel even after the
// file is closed.
func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.docMu.Lock()
	delete(s.docs, params.TextDocument.URI)
	s.docMu.Unlock()

	return s.publisher.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentURI(params.TextDocument.URI),
		Diagnostics: []protocol.Diagnostic{},
	})
}

// --- Analysis pipeline -----------------------------------------------------

// analyzeAndPublish is the single entry point shared by didOpen and
// didChange. It encapsulates the cache-lookup → parse → analyze → publish
// flow described in ADR-009. All errors are translated into a single
// "analysis failed" diagnostic rather than bubbled to the LSP client,
// because raising LSP errors causes some editors (notably Neovim) to
// disable the language server entirely on the next request.
func (s *Server) analyzeAndPublish(ctx context.Context, docURI uri.URI, languageID, content string, version uint32) error {
	format := chooseFormat(docURI, languageID)

	// Documents we cannot meaningfully analyze (e.g. .md, .yaml today)
	// receive an empty publish so stale diagnostics from a previous file
	// type assignment are wiped.
	if format == "" {
		s.docMu.Lock()
		delete(s.docs, docURI)
		s.docMu.Unlock()
		return s.publish(ctx, docURI, version, nil, nil)
	}

	key := cache.MakeKey(format, []byte(content))

	if findings, ok := s.cache.Get(key); ok {
		// Fast path: identical content seen earlier. We still need the
		// graph to render Hover ranges, so re-parse it cheaply (small
		// JSON / a single Go file). If parsing fails we degrade to the
		// findings-only render path: diagnostics still appear, just at
		// (0,0) ranges.
		graph := tryParse(s.parserFactory, format, content)
		s.storeDoc(docURI, languageID, format, content, key, graph, findings)
		return s.publish(ctx, docURI, version, graph, findings)
	}

	graph, parseErr := parseContent(s.parserFactory, format, content)
	if parseErr != nil {
		s.storeDoc(docURI, languageID, format, content, key, nil, nil)
		return s.publish(ctx, docURI, version, nil, []domain.Finding{
			{
				RuleName: "shingan_parse_error",
				Severity: domain.Warning,
				Message:  fmt.Sprintf("shingan: failed to parse document — %v", parseErr),
			},
		})
	}

	rules := s.analyzerFactory.CreateAll()
	findings := s.orchestrator.Analyze(graph, rules)

	if status := s.pythonHealth.Status(); !status.Healthy && status.CheckedAt.IsZero() == false {
		// Degraded-mode notice. Today this is purely informational
		// because no rule actually requires Python, but adding the
		// diagnostic now keeps the user-facing contract stable for when
		// Track P (LangGraph parser) lands.
		findings = append(findings, domain.Finding{
			RuleName: "shingan_degraded_mode",
			Severity: domain.Info,
			Message:  fmt.Sprintf("shingan: limited analysis — %s", status.Reason),
		})
	}

	s.cache.Add(key, findings)
	s.storeDoc(docURI, languageID, format, content, key, graph, findings)
	return s.publish(ctx, docURI, version, graph, findings)
}

// publish converts findings into LSP diagnostics and emits them. An empty
// slice is sent (rather than skipping the call) so the editor clears any
// stale diagnostics from a previous analysis run.
func (s *Server) publish(ctx context.Context, docURI uri.URI, version uint32, graph *domain.WorkflowGraph, findings []domain.Finding) error {
	diags := findingsToDiagnostics(graph, findings)
	return s.publisher.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentURI(docURI),
		Version:     version,
		Diagnostics: diags,
	})
}

// storeDoc persists the latest analyzed snapshot. It is always called
// after analysis completes (success or parse error) so Hover / CodeAction
// can find the doc, and so the next didChange knows the language ID.
func (s *Server) storeDoc(docURI uri.URI, languageID, format, content string, key cache.Key, graph *domain.WorkflowGraph, findings []domain.Finding) {
	s.docMu.Lock()
	s.docs[docURI] = &docState{
		languageID: languageID,
		format:     format,
		content:    content,
		hashKey:    key,
		graph:      graph,
		findings:   findings,
	}
	s.docMu.Unlock()
}

// snapshot returns the most recent docState for uri (read-locked). It is
// used by Hover / CodeAction to look up findings without holding the
// write lock during user-facing operations.
func (s *Server) snapshot(docURI uri.URI) (*docState, bool) {
	s.docMu.RLock()
	defer s.docMu.RUnlock()
	d, ok := s.docs[docURI]
	return d, ok
}

// --- Helpers ---------------------------------------------------------------

// chooseFormat maps an LSP document URI + languageID onto a parser format
// understood by ParserFactory. The mapping is deliberately conservative:
// only files whose extension OR languageID makes them obviously a workflow
// description are routed to a parser. Everything else returns "" and
// receives an empty diagnostics publish.
//
// Today's mapping:
//
//	*.json or languageId=json   → "json"
//	*.go                        → "adk-go"
//	*.py or languageId=python   → "langgraph" (ADR-011 main parser)
//	languageId=samurai          → "samurai" (rare, opt-in via VS Code config)
//
// We do not sniff JSON content for samurai vs json today; users must
// configure the file association explicitly. Tighten when SamuraiAI
// adoption justifies it.
//
// Per Codex iter2 P1 review: routing Python documents to LangGraph is
// required so hover/code-action/diagnostics work for the framework that
// ADR-011 makes Shingan's primary target. Without this mapping, opening
// a *.py LangGraph file via LSP yields an empty diagnostics publish.
func chooseFormat(u uri.URI, languageID string) string {
	ext := strings.ToLower(filepath.Ext(u.Filename()))
	switch {
	case ext == ".json" || strings.EqualFold(languageID, "json"):
		return "json"
	case ext == ".go" || strings.EqualFold(languageID, "go"):
		return "adk-go"
	case ext == ".py" || strings.EqualFold(languageID, "python"):
		return "langgraph"
	case strings.EqualFold(languageID, "samurai"):
		return "samurai"
	default:
		return ""
	}
}

// parseContent invokes the appropriate parser. Pulled out as a free
// function so analyzeAndPublish stays readable.
func parseContent(f *factory.ParserFactory, format, content string) (*domain.WorkflowGraph, error) {
	p, err := f.Create(format)
	if err != nil {
		return nil, err
	}
	return p.Parse([]byte(content))
}

// tryParse returns the parsed graph or nil on failure. Used on the cache-
// hit path where we want a graph for Hover ranges but cannot afford to
// abort if parsing has regressed since the cache entry was stored.
func tryParse(f *factory.ParserFactory, format, content string) *domain.WorkflowGraph {
	g, err := parseContent(f, format, content)
	if err != nil {
		return nil
	}
	return g
}
