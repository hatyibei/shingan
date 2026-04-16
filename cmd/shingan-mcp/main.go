// Command shingan-mcp exposes Shingan's workflow-graph static analyzer as a
// Model Context Protocol (MCP) server over stdio, so it can be invoked from
// Claude Desktop, Cursor, LangGraph Studio, or any other MCP-capable client.
//
// The binary is a thin wiring layer on top of the existing Onion
// architecture: it constructs the factories / orchestrator used by the CLI
// (cmd/shingan/analyze.go) and registers four tools on a new MCP server.
// No new core logic lives here; see application, domain and
// infrastructure/factory for the actual analysis machinery.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

// version is overridden at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if err := run(context.Background()); err != nil {
		log.SetFlags(0)
		log.SetPrefix("shingan-mcp: ")
		log.Println(err)
		os.Exit(1)
	}
}

// run constructs dependencies, wires the four tools, and starts the stdio
// MCP loop. It returns when the client disconnects or the context is cancelled.
func run(ctx context.Context) error {
	deps := &toolDeps{
		analyzerFactory: factory.NewAnalyzerFactory(),
		parserFactory:   factory.NewParserFactory(),
		orchestrator:    application.NewAnalysisOrchestrator(),
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "shingan",
		Version: version,
	}, nil)

	registerTools(server, deps)

	return server.Run(ctx, &mcp.StdioTransport{})
}
