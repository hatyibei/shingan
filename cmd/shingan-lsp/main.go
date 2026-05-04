// Command shingan-lsp is a Language Server Protocol server for Shingan,
// providing IDE-grade workflow-graph diagnostics over stdio JSON-RPC.
//
// Usage:
//
//	shingan-lsp                    # talk LSP on stdin/stdout
//	shingan-lsp --version          # print version and exit
//
// The server is a thin wiring layer over the same building blocks the CLI
// uses (cmd/shingan/analyze.go) — there is no analysis logic here, just
// the LSP-side glue: protocol bookkeeping, an SHA-256 LRU diff cache, and
// the Python health probe that drives degraded mode.
//
// See ADR-009 for the 3-layer defensive architecture (diff cache, long-
// lived Python subprocess, degraded mode) and docs/lsp.md for editor setup.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.uber.org/zap"
)

// version is overridden at build time via -ldflags "-X main.serverVersion=...".
// We re-export it as serverVersion so server.go can include it in the
// InitializeResult ServerInfo block.
var serverVersion = "dev"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		log.SetFlags(0)
		log.SetPrefix("shingan-lsp: ")
		log.Println(err)
		os.Exit(1)
	}
}

// run is the testable entry point. It binds the Server to a stdio jsonrpc2
// stream and blocks until the client disconnects, the context is cancelled,
// or the server's Exit handler tears the process down.
//
// Splitting main() / run() keeps the production entry small (it owns
// os.Args and exit codes) while letting tests drive the LSP loop with a
// pair of os.Pipe handles instead of stdin/stdout.
func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	for _, a := range args {
		switch a {
		case "--version", "-v":
			fmt.Fprintln(stdout, serverVersion)
			return nil
		case "--help", "-h":
			fmt.Fprintln(stdout, helpText)
			return nil
		}
	}

	logger, err := buildLogger(stderr)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	stream := jsonrpc2.NewStream(struct {
		io.Reader
		io.Writer
		io.Closer
	}{
		Reader: stdin,
		Writer: stdout,
		Closer: io.NopCloser(nil),
	})

	srv := NewServer(nil) // publisher attached below once we have the client

	// protocol.NewServer wires the dispatcher to our Server and returns
	// the client handle (used for outbound notifications like
	// publishDiagnostics). We have to attach the publisher after the call
	// because the client depends on the connection it creates.
	ctx, conn, client := protocol.NewServer(ctx, srv, stream, logger)
	srv.publisher = client

	<-conn.Done()
	if err := conn.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("lsp connection: %w", err)
	}
	return nil
}

// buildLogger configures a zap logger that writes to stderr. We use stderr
// (not stdout) because the LSP framing reserves stdout for JSON-RPC traffic
// — any stray write there corrupts the protocol stream.
func buildLogger(stderr io.Writer) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stderr"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	cfg.DisableStacktrace = true
	// We only emit warnings and above by default to keep stderr quiet
	// when run from an editor; users can crank verbosity up via env.
	cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	_ = stderr // hook reserved for future structured logging targets
	return logger, nil
}

const helpText = `shingan-lsp — Language Server Protocol server for Shingan

Usage:
  shingan-lsp           speak LSP on stdin/stdout (default)
  shingan-lsp --version print version and exit
  shingan-lsp --help    print this message

Configure your editor to launch this binary; see docs/lsp.md for
VS Code, Cursor, Neovim, Helix and Zed snippets.`
