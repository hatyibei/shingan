package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/hatyibei/shingan/infrastructure/parser"
)

// recordingPublisher captures every PublishDiagnostics call so tests can
// assert on the diagnostics the server emitted. We need this because the
// LSP "result" of an analysis flow is a notification, not a return value.
type recordingPublisher struct {
	mu    sync.Mutex
	calls []protocol.PublishDiagnosticsParams
}

func (r *recordingPublisher) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Defensive copy: clients otherwise see the slice mutate as the
	// server runs subsequent analyses. We preserve nil-vs-empty exactly
	// (the server distinguishes them: empty means "clear diagnostics").
	cp := *params
	if params.Diagnostics != nil {
		cp.Diagnostics = make([]protocol.Diagnostic, len(params.Diagnostics))
		copy(cp.Diagnostics, params.Diagnostics)
	}
	r.calls = append(r.calls, cp)
	return nil
}

func (r *recordingPublisher) snapshot() []protocol.PublishDiagnosticsParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]protocol.PublishDiagnosticsParams(nil), r.calls...)
}

// readBuggyFixture returns the canonical buggy.json from the repo's
// testdata/. Used to exercise the full parse + analyze pipeline.
func readBuggyFixture(t *testing.T) string {
	t.Helper()
	// We're running from cmd/shingan-lsp/, so testdata/buggy.json is two
	// levels up. Resolve via filepath.Abs to be robust against future
	// changes in the test runner's CWD.
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "buggy.json"))
	if err != nil {
		t.Fatalf("read buggy.json: %v", err)
	}
	return string(raw)
}

func docURI(t *testing.T, path string) uri.URI {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs %q: %v", path, err)
	}
	return uri.File(abs)
}

func TestServer_Initialize_AdvertisesCapabilities(t *testing.T) {
	t.Parallel()

	srv := NewServer(&recordingPublisher{})
	res, err := srv.Initialize(context.Background(), &protocol.InitializeParams{})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if res.Capabilities.TextDocumentSync != protocol.TextDocumentSyncKindFull {
		t.Errorf("expected TextDocumentSyncKindFull, got %v", res.Capabilities.TextDocumentSync)
	}
	if res.Capabilities.HoverProvider == nil {
		t.Error("expected HoverProvider to be set")
	}
	if res.Capabilities.CodeActionProvider == nil {
		t.Error("expected CodeActionProvider to be set")
	}
	if res.ServerInfo == nil || res.ServerInfo.Name != "shingan-lsp" {
		t.Errorf("expected ServerInfo.Name='shingan-lsp', got %+v", res.ServerInfo)
	}
}

func TestServer_DidOpen_PublishesFindingsForBuggyJSON(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)

	if _, err := srv.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}

	content := readBuggyFixture(t)
	docURI := docURI(t, "buggy.json")
	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        docURI,
			LanguageID: protocol.JSONLanguage,
			Version:    1,
			Text:       content,
		},
	}); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	calls := pub.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 PublishDiagnostics call, got %d", len(calls))
	}
	got := calls[0]
	if got.URI != protocol.DocumentURI(docURI) {
		t.Errorf("URI mismatch: got %q want %q", got.URI, docURI)
	}
	if len(got.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics for buggy.json, got 0")
	}

	// buggy.json contains an orphan_llm node and a loop without
	// max_iterations — at minimum we should see unreachable_node and
	// loop_guard. Asserting their presence guards against regressions in
	// the diagnostics pipeline (parse → analyze → translate).
	rules := map[string]bool{}
	for _, d := range got.Diagnostics {
		if code, ok := d.Code.(string); ok {
			rules[code] = true
		}
	}
	for _, expected := range []string{"unreachable_node", "loop_guard"} {
		if !rules[expected] {
			t.Errorf("expected diagnostic for rule %q, got rules=%v", expected, ruleNames(rules))
		}
	}
}

func TestServer_DidChange_UsesCacheOnIdenticalContent(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)

	content := readBuggyFixture(t)
	docURI := docURI(t, "buggy_cache.json")

	// First open: cold path.
	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: protocol.JSONLanguage, Version: 1, Text: content,
		},
	}); err != nil {
		t.Fatal(err)
	}

	firstLen := len(pub.snapshot())

	// Subsequent didChange with identical text MUST hit the cache; the
	// only side effect is a fresh publish (so the editor stays in sync).
	if err := srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: content}},
	}); err != nil {
		t.Fatal(err)
	}

	calls := pub.snapshot()
	if len(calls) != firstLen+1 {
		t.Fatalf("expected an extra publish call after didChange; before=%d after=%d", firstLen, len(calls))
	}
	if len(calls[firstLen].Diagnostics) != len(calls[firstLen-1].Diagnostics) {
		t.Errorf("expected identical diagnostic count on cache hit; first=%d second=%d",
			len(calls[firstLen-1].Diagnostics), len(calls[firstLen].Diagnostics))
	}
}

func TestServer_DidClose_ClearsDiagnostics(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)
	docURI := docURI(t, "buggy_close.json")

	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: protocol.JSONLanguage, Version: 1, Text: readBuggyFixture(t),
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := srv.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	}); err != nil {
		t.Fatal(err)
	}

	calls := pub.snapshot()
	last := calls[len(calls)-1]
	if last.URI != protocol.DocumentURI(docURI) {
		t.Errorf("close URI mismatch: got %q", last.URI)
	}
	if last.Diagnostics == nil {
		t.Fatalf("expected explicit empty (non-nil) diagnostics on close, got nil")
	}
	if len(last.Diagnostics) != 0 {
		t.Errorf("expected empty diagnostics on close, got %d", len(last.Diagnostics))
	}
}

func TestServer_DidOpen_UnsupportedExtensionPublishesEmpty(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)
	docURI := docURI(t, "irrelevant.md")

	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: "markdown", Version: 1, Text: "# unrelated",
		},
	}); err != nil {
		t.Fatal(err)
	}

	calls := pub.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (empty publish), got %d", len(calls))
	}
	if len(calls[0].Diagnostics) != 0 {
		t.Errorf("expected 0 diagnostics for unsupported file, got %d", len(calls[0].Diagnostics))
	}
}

func TestServer_DidOpen_MalformedJSONReportsParseError(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)
	docURI := docURI(t, "broken.json")

	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: protocol.JSONLanguage, Version: 1, Text: "{ not valid json ",
		},
	}); err != nil {
		t.Fatal(err)
	}

	calls := pub.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 publish on malformed input, got %d", len(calls))
	}
	codes := ruleNamesFromDiagnostics(calls[0].Diagnostics)
	if !codes["shingan_parse_error"] {
		t.Errorf("expected shingan_parse_error diagnostic, got %v", codes)
	}
}

func TestServer_Hover_ReturnsDetailsForFinding(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)
	docURI := docURI(t, "buggy_hover.json")

	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: protocol.JSONLanguage, Version: 1, Text: readBuggyFixture(t),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Findings without SourcePos always map to (0,0)-(0,1). Hovering at
	// (0,0) therefore overlaps every finding.
	hover, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 0},
		},
	})
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected a non-nil Hover result")
	}
	if !strings.Contains(hover.Contents.Value, "**shingan:") {
		t.Errorf("expected hover markdown to mention shingan, got: %s", hover.Contents.Value)
	}
}

func TestServer_Hover_NoMatchReturnsNil(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)
	docURI := docURI(t, "no_doc.json")

	hover, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 5, Character: 5},
		},
	})
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover != nil {
		t.Errorf("expected nil Hover for unknown doc, got %+v", hover)
	}
}

func TestServer_CodeAction_ReturnsSuggestionWhenAvailable(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)
	docURI := docURI(t, "buggy_action.json")

	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: protocol.JSONLanguage, Version: 1, Text: readBuggyFixture(t),
		},
	}); err != nil {
		t.Fatal(err)
	}

	actions, err := srv.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 5},
		},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	if actions == nil {
		t.Fatal("expected non-nil actions slice (empty allowed, nil forbidden)")
	}
	// buggy.json triggers loop_guard, which has a non-empty Suggestion;
	// at least one CodeAction should surface as a result.
	if len(actions) == 0 {
		t.Fatalf("expected at least one suggestion action, got 0")
	}
	for _, a := range actions {
		if a.Kind != protocol.QuickFix {
			t.Errorf("expected QuickFix kind, got %q", a.Kind)
		}
		if !strings.HasPrefix(a.Title, "shingan:") {
			t.Errorf("expected title to start with 'shingan:', got %q", a.Title)
		}
	}
}

func TestServer_DidOpen_EmitsDegradedModeWhenPythonUnhealthy(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)

	// Replace the embedded probe with one pointed at a non-existent
	// binary so it deterministically reports unhealthy. We force a
	// synchronous probe BEFORE didOpen — mirroring what the real
	// Initialized handler does — to guarantee Status() is non-zero by
	// the time analyzeAndPublish inspects it.
	srv.pythonHealth = parser.NewPythonHealth(parser.WithExecutable("/nonexistent/python-binary-xyz"))
	if _, err := srv.pythonHealth.RunCheck(context.Background()); err == nil {
		t.Fatal("expected the bogus python health probe to fail")
	}

	docURI := docURI(t, "buggy_degraded.json")
	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        docURI,
			LanguageID: protocol.JSONLanguage,
			Version:    1,
			Text:       readBuggyFixture(t),
		},
	}); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	calls := pub.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 publish, got %d", len(calls))
	}
	codes := ruleNamesFromDiagnostics(calls[0].Diagnostics)
	if !codes["shingan_degraded_mode"] {
		t.Errorf("expected shingan_degraded_mode diagnostic when python is unhealthy; got rules=%v",
			ruleNames(codes))
	}
	// Functional rules MUST still fire — degraded mode is additive, not
	// a replacement. buggy.json reliably trips loop_guard.
	if !codes["loop_guard"] {
		t.Errorf("expected loop_guard to still fire in degraded mode; got rules=%v",
			ruleNames(codes))
	}
}

func TestServer_DidOpen_NoDegradedDiagnosticWhenProbeNotRun(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)

	// Do NOT call srv.Initialized / RunCheck. The probe's CheckedAt
	// remains zero, which the server treats as "we don't yet know" and
	// therefore omits the degraded-mode diagnostic. This pins the
	// "first-analysis silence" invariant — see Initialized's comment in
	// server.go for the rationale (the LSP entry point ALWAYS calls
	// RunCheck synchronously before any didOpen lands).
	docURI := docURI(t, "buggy_unprobed.json")
	if err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI: docURI, LanguageID: protocol.JSONLanguage, Version: 1, Text: readBuggyFixture(t),
		},
	}); err != nil {
		t.Fatal(err)
	}
	codes := ruleNamesFromDiagnostics(pub.snapshot()[0].Diagnostics)
	if codes["shingan_degraded_mode"] {
		t.Errorf("did not expect degraded-mode diagnostic before probe ran; got rules=%v",
			ruleNames(codes))
	}
}

func TestServer_LifecycleSequence(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	srv := NewServer(pub)

	// initialize → initialized → shutdown should all succeed without
	// errors and leave the server's internal state consistent.
	if _, err := srv.Initialize(context.Background(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	// Initialized runs the python health probe synchronously; no sleep
	// needed for determinism, but keep the call to exercise the full
	// startup sequence.
	if err := srv.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !srv.shutdownRequested {
		t.Error("expected shutdownRequested=true after Shutdown call")
	}
}

func TestRun_VersionFlag(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	if err := run(context.Background(), []string{"--version"}, strings.NewReader(""), &stdout, &strings.Builder{}); err != nil {
		t.Fatalf("run --version: %v", err)
	}
	if !strings.Contains(stdout.String(), serverVersion) {
		t.Errorf("expected stdout to contain version, got %q", stdout.String())
	}
}

func TestRun_HelpFlag(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	if err := run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &strings.Builder{}); err != nil {
		t.Fatalf("run --help: %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("expected help text on stdout, got %q", stdout.String())
	}
}

// ruleNames lifts a presence map's keys into a slice for stable error messages.
func ruleNames(seen map[string]bool) []string {
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func ruleNamesFromDiagnostics(diags []protocol.Diagnostic) map[string]bool {
	out := map[string]bool{}
	for _, d := range diags {
		if code, ok := d.Code.(string); ok {
			out[code] = true
		}
	}
	return out
}

// TestChooseFormat_Python verifies the Codex iter2 P1 fix: *.py files and
// languageId=python must route to the LangGraph parser, otherwise the LSP
// produces empty diagnostics for the framework that ADR-011 makes
// Shingan's primary target.
func TestChooseFormat_Python(t *testing.T) {
	cases := []struct {
		name       string
		uri        string
		languageID string
		want       string
	}{
		{"py extension", "file:///tmp/agent.py", "", "langgraph"},
		{"python languageID", "file:///tmp/no-ext", "python", "langgraph"},
		{"PYTHON case-insensitive", "file:///tmp/no-ext", "PYTHON", "langgraph"},
		{"json still json", "file:///tmp/x.json", "", "json"},
		{"go still adk-go", "file:///tmp/x.go", "", "adk-go"},
		{"unknown extension", "file:///tmp/x.txt", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseFormat(uri.New(tc.uri), tc.languageID)
			if got != tc.want {
				t.Errorf("chooseFormat(%q, %q) = %q, want %q", tc.uri, tc.languageID, got, tc.want)
			}
		})
	}
}
