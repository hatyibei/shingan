package main

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/application"
	"github.com/spf13/cobra"
)

// newListRulesCmd builds the `shingan list-rules` subcommand. Lists
// every built-in rule with a one-line snippet of its explanation so
// users can pick the right one for `# shingan: ignore <rule>` /
// `.shingan.yaml` overrides without leaving the terminal.
func newListRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-rules",
		Short: "List every built-in rule with a one-line summary",
		Long: `Lists every built-in rule registered in the analyzer factory, in the
form  <rule_name>  <one-line summary>.

Use the rule names with --policy / # shingan: ignore / SARIF query
filters. For the full multi-paragraph explanation of a rule, run:

	shingan explain <rule_name>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-30s  %s\n", "RULE", "SUMMARY")
			fmt.Fprintf(out, "%-30s  %s\n", strings.Repeat("-", 30), strings.Repeat("-", 60))
			for _, name := range application.KnownRuleNames() {
				text, _ := application.ExplainRule(name)
				summary := firstLine(text)
				fmt.Fprintf(out, "%-30s  %s\n", name, summary)
			}
			return nil
		},
	}
}

// newExplainCmd builds the `shingan explain <rule>` subcommand.
// Prints the full explanation text for a rule. Mirrors the MCP
// `shingan_explain_rule` tool so terminal users have parity with
// LangGraph Studio / Cursor / Claude Desktop's AI assistants.
func newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <rule_name>",
		Short: "Print the full explanation for a built-in rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, ok := application.ExplainRule(args[0])
			if !ok {
				known := application.KnownRuleNames()
				return fmt.Errorf("unknown rule %q. Known rules:\n  %s",
					args[0], strings.Join(known, "\n  "))
			}
			fmt.Fprintln(cmd.OutOrStdout(), text)
			return nil
		},
	}
}

// firstLine returns the first non-empty line of s, trimmed. Used by
// `list-rules` to surface a digestible summary per rule.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip the leading `<rule_name> — ` prefix our explanations
		// open with so the table doesn't repeat the rule column.
		if idx := strings.Index(line, " — "); idx > 0 {
			return strings.TrimSpace(line[idx+len(" — "):])
		}
		return line
	}
	return ""
}
