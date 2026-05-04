package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"go.uber.org/zap"
)

// pipeStream pairs an io.Reader and io.Writer into a jsonrpc2.Stream-compatible
// transport, glueing two in-memory ends together with bytes.Pipe pairs. We
// avoid os.Pipe to stay portable across Windows test runners.
type bidiPipe struct {
	rd *pipeReader
	wr *pipeWriter
}

func (p *bidiPipe) Read(b []byte) (int, error)  { return p.rd.Read(b) }
func (p *bidiPipe) Write(b []byte) (int, error) { return p.wr.Write(b) }
func (p *bidiPipe) Close() error {
	_ = p.wr.Close()
	return p.rd.Close()
}

// pipeReader / pipeWriter wrap channels carrying single-frame JSON-RPC writes.
// We don't need a full bytes.Pipe — the LSP stream framing handles batching.
type pipeReader struct {
	mu     sync.Mutex
	buf    []byte
	notify chan struct{}
	closed bool
}

type pipeWriter struct {
	target *pipeReader
}

func (p *pipeReader) Read(b []byte) (int, error) {
	for {
		p.mu.Lock()
		if len(p.buf) > 0 {
			n := copy(b, p.buf)
			p.buf = p.buf[n:]
			p.mu.Unlock()
			return n, nil
		}
		if p.closed {
			p.mu.Unlock()
			return 0, errClosedPipe
		}
		p.mu.Unlock()
		<-p.notify
	}
}

func (p *pipeReader) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.notify)
	p.mu.Unlock()
	return nil
}

func (p *pipeWriter) Write(b []byte) (int, error) {
	p.target.mu.Lock()
	if p.target.closed {
		p.target.mu.Unlock()
		return 0, errClosedPipe
	}
	p.target.buf = append(p.target.buf, b...)
	p.target.mu.Unlock()
	select {
	case p.target.notify <- struct{}{}:
	default:
	}
	return len(b), nil
}

func (p *pipeWriter) Close() error { return nil }

var errClosedPipe = pipeError("pipe closed")

type pipeError string

func (e pipeError) Error() string { return string(e) }

// newDuplexPipe returns two endpoints whose writes are visible to the
// counterpart's reads. Used to wire a server and client jsonrpc2 stream
// together without spawning a process.
func newDuplexPipe() (*bidiPipe, *bidiPipe) {
	a := &pipeReader{notify: make(chan struct{}, 1)}
	b := &pipeReader{notify: make(chan struct{}, 1)}
	endA := &bidiPipe{rd: a, wr: &pipeWriter{target: b}}
	endB := &bidiPipe{rd: b, wr: &pipeWriter{target: a}}
	return endA, endB
}

// stubClient absorbs server → client notifications. We only care about
// PublishDiagnostics for these tests; everything else is satisfied by the
// embedded protocol.Client default values via baseClient.
type stubClient struct {
	baseClient

	mu          sync.Mutex
	diagnostics []*protocol.PublishDiagnosticsParams
	notified    chan struct{}
}

func (c *stubClient) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	c.mu.Lock()
	cp := *params
	if params.Diagnostics != nil {
		cp.Diagnostics = make([]protocol.Diagnostic, len(params.Diagnostics))
		copy(cp.Diagnostics, params.Diagnostics)
	}
	c.diagnostics = append(c.diagnostics, &cp)
	c.mu.Unlock()
	select {
	case c.notified <- struct{}{}:
	default:
	}
	return nil
}

func (c *stubClient) wait(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-c.notified:
	case <-time.After(d):
		t.Fatalf("timed out waiting for diagnostics")
	}
}

// TestIntegration_Handshake performs a real LSP-over-pipes initialize +
// didChange + publishDiagnostics cycle. This exercises the dispatch glue
// (protocol.NewServer / NewClient, ServerHandler) that the unit tests in
// main_test.go bypass by calling Server methods directly.
func TestIntegration_Handshake(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverEnd, clientEnd := newDuplexPipe()

	// Server side: real Server, real protocol.NewServer. The publisher we
	// bind here is the protocol.Client returned by the call.
	srv := NewServer(nil)
	logger := zap.NewNop()
	serverStream := jsonrpc2.NewStream(serverEnd)

	_, serverConn, serverClient := protocol.NewServer(ctx, srv, serverStream, logger)
	srv.publisher = serverClient

	// Client side: stubClient handles inbound notifications, the returned
	// `serverProxy` is what we Call() to send requests/notifications.
	clientStub := &stubClient{notified: make(chan struct{}, 16)}
	clientStream := jsonrpc2.NewStream(clientEnd)
	_, clientConn, serverProxy := protocol.NewClient(ctx, clientStub, clientStream, logger)
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()

	// 1. initialize
	initRes, err := serverProxy.Initialize(ctx, &protocol.InitializeParams{})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if initRes.ServerInfo == nil || initRes.ServerInfo.Name != "shingan-lsp" {
		t.Fatalf("unexpected ServerInfo: %+v", initRes.ServerInfo)
	}
	// TextDocumentSync is declared as interface{} (a TextDocumentSyncKind
	// or TextDocumentSyncOptions), and JSON decoding always lands in a
	// numeric type. We assert equality after coercing to float64, the
	// shape every JSON decoder produces for a TextDocumentSyncKind.
	if !syncKindIs(initRes.Capabilities.TextDocumentSync, protocol.TextDocumentSyncKindFull) {
		t.Errorf("expected SyncKindFull, got %v (%T)", initRes.Capabilities.TextDocumentSync, initRes.Capabilities.TextDocumentSync)
	}

	// 2. initialized
	if err := serverProxy.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("Initialized: %v", err)
	}

	// 3. didOpen with the buggy fixture so we get diagnostics back.
	docURI := uri.New("file:///tmp/integration_buggy.json")
	if err := serverProxy.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        docURI,
			LanguageID: protocol.JSONLanguage,
			Version:    1,
			Text:       readBuggyFixture(t),
		},
	}); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	clientStub.wait(t, 5*time.Second)

	clientStub.mu.Lock()
	if len(clientStub.diagnostics) == 0 {
		clientStub.mu.Unlock()
		t.Fatal("expected at least one PublishDiagnostics notification")
	}
	got := clientStub.diagnostics[0]
	clientStub.mu.Unlock()

	if got.URI != protocol.DocumentURI(docURI) {
		t.Errorf("URI mismatch: got %q want %q", got.URI, docURI)
	}
	if len(got.Diagnostics) == 0 {
		t.Error("expected diagnostics from buggy.json")
	}

	// 4. shutdown — ensure the server cleanly responds and we observe the
	// shutdownRequested flag flip.
	if err := serverProxy.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Force the test to finish promptly even if Done isn't yet signalled.
	if !srv.shutdownRequested {
		// Unset flag is fine racing-wise: the dispatch may not have
		// returned by the time we observe — wait briefly.
		deadline := time.Now().Add(time.Second)
		for !srv.shutdownRequested && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !srv.shutdownRequested {
			t.Error("expected shutdownRequested=true after Shutdown")
		}
	}

	// Drop sentinel to release any blocked reads on either end and let
	// the goroutines wind down quickly. test-cleanup purpose only.
	_ = serverEnd.Close()
	_ = clientEnd.Close()
}

// syncKindIs returns true when v decodes back to the requested
// TextDocumentSyncKind. The protocol package types this field as
// interface{}, so we cope with float64 (typical JSON decoder output) and
// the strongly-typed value.
func syncKindIs(v interface{}, want protocol.TextDocumentSyncKind) bool {
	switch x := v.(type) {
	case protocol.TextDocumentSyncKind:
		return x == want
	case float64:
		return protocol.TextDocumentSyncKind(x) == want
	case int:
		return protocol.TextDocumentSyncKind(x) == want
	}
	return false
}
