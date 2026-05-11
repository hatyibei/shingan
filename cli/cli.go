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
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// exitCodeError is a typed error returned from a subcommand's RunE
// when the work succeeded but the analysis result requires a non-zero
// process exit code (Warning=1, Critical=2). cli.Run translates this
// into the exit code without process-killing the caller — that's the
// contract that lets plugin wrapper binaries and in-process tests
// drive analyze via cli.Run. Codex Slice B #1: pre-fix, analyze
// called os.Exit(code) directly in RunE.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

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
	cmd.AddCommand(newVersionCmd())

	return cmd
}

// Run executes the root cobra command with the supplied argv (typically
// `os.Args[1:]`). Returns the process exit code: 0 on success, 1 on
// any real error, or the analysis exit code (1=Warning, 2=Critical)
// when a finding-based exitCodeError surfaces from analyze.
//
// Real errors are printed to stderr before returning;
// finding-based exits print only the report itself (which the
// subcommand already wrote to stdout). Plugin wrapper binaries
// typically just write `os.Exit(cli.Run(os.Args[1:]))`.
func Run(args []string) int {
	root := NewRootCmd()
	// Silence cobra's auto-print of errors and usage so we can
	// distinguish finding-based exits (no extra output) from real
	// errors (Error: <msg>) ourselves. Applied to root + every
	// subcommand so the behaviour is uniform.
	silenceErrors(root)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var ec *exitCodeError
		if errors.As(err, &ec) {
			return ec.code
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// silenceErrors walks the command tree disabling cobra's automatic
// "Error: ..." print and usage-on-error display. We render real
// errors ourselves in Run() and finding-based exitCodeError values
// produce no extra output.
func silenceErrors(cmd *cobra.Command) {
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	for _, c := range cmd.Commands() {
		silenceErrors(c)
	}
}
