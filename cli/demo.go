package cli

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// embeddedDemo bundles a tiny but representative WorkflowGraph so
// `shingan demo` can run end-to-end without the user supplying any
// input file. The sample triggers loop_guard (Critical) and
// unreachable_node (Warning) on purpose.
//
//go:embed embedded/demo.json
var embeddedDemo embed.FS

// newDemoCmd builds the `shingan demo` subcommand: a zero-config
// smoke test that exercises the full analyzer pipeline against the
// embedded sample and prints a human-readable report. The exit code
// matches `shingan analyze` semantics (0/1/2) so users can verify the
// install behaves correctly under CI as well.
func newDemoCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run shingan against a built-in sample workflow (no input file needed)",
		Long: `demo writes a small built-in workflow to a temp file and runs the
full analyzer pipeline against it. Use this to verify your install
and to see what a typical findings report looks like.

The bundled sample intentionally contains a LoopAgent without
MaxIterations (Critical) and an unreachable node (Warning), so the
demo exits with code 2.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := embeddedDemo.ReadFile("embedded/demo.json")
			if err != nil {
				return fmt.Errorf("read embedded demo: %w", err)
			}
			tmpDir, err := os.MkdirTemp("", "shingan-demo-*")
			if err != nil {
				return fmt.Errorf("mkdir tmp: %w", err)
			}
			defer os.RemoveAll(tmpDir)
			tmpPath := filepath.Join(tmpDir, "demo.json")
			if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
				return fmt.Errorf("write demo: %w", err)
			}

			flags := &analyzeFlags{
				input:  tmpPath,
				format: "json",
				output: output,
				stdout: cmd.OutOrStdout(),
				stderr: cmd.ErrOrStderr(),
			}

			fmt.Fprintln(flags.stderr, "shingan demo: analyzing the built-in sample")
			fmt.Fprintln(flags.stderr, "  (loop without MaxIterations + one unreachable node)")
			fmt.Fprintln(flags.stderr)

			code, err := executeAnalyze(flags)
			if err != nil {
				return err
			}

			fmt.Fprintln(flags.stderr)
			fmt.Fprintf(flags.stderr, "shingan demo: exit code %d  (0=clean, 1=warning, 2=critical)\n", code)
			fmt.Fprintln(flags.stderr, "Next:  shingan analyze --input <path/to/workflow.json>  (or --format adk-go, langgraph, n8n, crewai)")

			if code != 0 {
				return &exitCodeError{code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "markdown", "Output format: json, markdown, or sarif")
	return cmd
}
