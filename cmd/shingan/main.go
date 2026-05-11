// Package main is the entry point for the shingan CLI.
// It wires together cobra commands with application and infrastructure layers,
// following Onion Architecture: this package contains DI wiring only — no
// business logic lives here.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd builds the root cobra.Command for the shingan CLI.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shingan",
		Short: "Shingan — AI Agent Workflow Static Analyzer",
		Long: `Shingan (心眼) detects structural bugs in AI agent workflows
before they reach production.

Supported analyses:
  • Cycle detection (infinite-loop risk)
  • Unreachable node detection (dead code)
  • Error handler checker (missing failure paths)`,
	}

	cmd.AddCommand(newAnalyzeCmd())
	cmd.AddCommand(newListRulesCmd())
	cmd.AddCommand(newExplainCmd())
	cmd.AddCommand(newRulesCmd())

	return cmd
}
