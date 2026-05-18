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
// shingan ships with (analyze, demo, list-rules, explain, rules,
// version). External wrapper binaries call this when they need to add
// their own subcommands before invoking Execute().
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shingan",
		Short: "Shingan — AI Agent Workflow Static Analyzer",
		Long: `Shingan (心眼) catches structural bugs in AI agent workflows
before they ever run — infinite loops, unreachable nodes, missing error
handlers, runaway-cost paths, prompt-injection sinks, PII leak paths,
code-execution from LLM output, and more.

20+ built-in rules across LangGraph, CrewAI, ADK-Go, n8n, and native JSON.
Output: markdown, JSON, or SARIF (GitHub Code Scanning compatible).`,
		Example: `  # Run a built-in sample (no setup, no input file)
  shingan demo

  # Analyze a JSON workflow
  shingan analyze --input workflow.json --output markdown

  # Analyze ADK-Go agents in a directory
  shingan analyze --format adk-go --input ./agents/

  # Analyze a LangGraph project (needs `+"`pip install langgraph`"+`)
  shingan analyze --format langgraph --input ./agents/

  # Show every rule the binary knows about
  shingan list-rules`,
		// When invoked with zero args, cobra's default RunE prints the
		// full usage page — a wall of text that hides the "what do I
		// type first?" answer. Replace it with a 4-line guided banner
		// pointing at `demo` (the only zero-config entry point).
		Run: func(cmd *cobra.Command, args []string) {
			out := cmd.OutOrStderr()
			fmt.Fprintln(out, "Shingan — AI Agent Workflow Static Analyzer")
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Get started:")
			fmt.Fprintln(out, "  shingan demo                          run a built-in example (no setup)")
			fmt.Fprintln(out, "  shingan analyze --input workflow.json analyze a single JSON workflow")
			fmt.Fprintln(out, "  shingan --help                        list every command and flag")
		},
	}

	cmd.AddCommand(newAnalyzeCmd())
	cmd.AddCommand(newDemoCmd())
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
