// Package cli is the importable runtime for the Shingan CLI. It
// exports `Run` and `NewRootCmd` so plugin wrapper binaries can
// embed the official shingan command tree by importing this package,
// while `cmd/shingan/main.go` remains a thin entry point.
//
// Plugin authoring:
//
//	// cmd/shingan-with-my-plugins/main.go
//	package main
//
//	import (
//	    "os"
//
//	    _ "github.com/your-org/your-plugin-repo" // side-effect: registers rule
//	    "github.com/hatyibei/shingan/cli"
//	)
//
//	func main() { os.Exit(cli.Run(os.Args[1:])) }
//
// See `examples/plugin-template/cmd/shingan-with-plugins/` for the
// canonical wrapper, and `docs/plugin-sdk.md` for the full Plugin SDK
// roadmap.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the root cobra command tree with every subcommand
// shingan ships with (analyze, list-rules, explain, rules). External
// wrapper binaries call this when they need to add their own
// subcommands before invoking Execute().
func NewRootCmd() *cobra.Command {
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

// Run executes the root cobra command with the supplied argv (typically
// `os.Args[1:]`). It returns an exit code suitable for `os.Exit`:
// 0 on success, 1 on any error returned from the command tree.
//
// Errors are printed to stderr before returning. Plugin wrapper
// binaries typically just write `os.Exit(cli.Run(os.Args[1:]))`.
func Run(args []string) int {
	root := NewRootCmd()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
