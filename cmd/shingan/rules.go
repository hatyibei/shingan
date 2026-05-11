package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/application"
	infraFactory "github.com/hatyibei/shingan/infrastructure/factory"
	"github.com/spf13/cobra"
)

// newRulesCmd builds the `shingan rules` subcommand. It emits the full
// rule catalog (Name / Severity / Fixable / Frameworks / Tags /
// Stability / DocsURL / Description) in either a JSON form (for IDEs,
// docs renderers, the shingan.dev catalog page) or a plain-text table
// for terminal users.
//
// This is the v0.x foundation for ADR-015 Plugin SDK: external rule
// authors will ship a RuleManifest alongside their `init()`-registered
// rule, and the same catalog renderer here will surface them in the
// same output without any other change. Until then, the catalog is
// derived from `application.ListRuleManifests`, which merges runtime
// rule metadata with the static table in application/rule_catalog.go.
func newRulesCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List every built-in rule with metadata (JSON or table)",
		Long: `Lists every built-in rule registered in the analyzer factory.

Output formats:
  table  (default) — terminal-friendly table
  json             — machine-readable catalog suitable for IDEs,
                     docs renderers, and CI policy generators

For the multi-paragraph explanation of a specific rule, run:

    shingan explain <rule_name>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Built-in rules only — plugin rules are merged inside
			// ListRuleManifests by reading the plugin.RegisteredRules
			// table. Passing only built-ins here keeps the layering
			// clean (catalog renderer doesn't need to know how
			// plugin discovery happens).
			rules := infraFactory.NewAnalyzerFactory().CreateAll()
			catalog := application.ListRuleManifests(rules)
			out := cmd.OutOrStdout()
			switch strings.ToLower(format) {
			case "json":
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(catalog)
			case "", "table":
				fmt.Fprintf(out, "%-30s  %-10s  %-15s  %s\n", "RULE", "SEVERITY", "FRAMEWORKS", "DESCRIPTION")
				fmt.Fprintf(out, "%-30s  %-10s  %-15s  %s\n",
					strings.Repeat("-", 30), strings.Repeat("-", 10),
					strings.Repeat("-", 15), strings.Repeat("-", 60))
				for _, m := range catalog {
					frameworks := strings.Join(m.Frameworks, ",")
					if frameworks == "" {
						frameworks = "-"
					}
					fmt.Fprintf(out, "%-30s  %-10s  %-15s  %s\n",
						m.Name, m.SeverityStr, frameworks, truncate(m.Description, 60))
				}
				return nil
			default:
				return fmt.Errorf("unknown --format %q (supported: table, json)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "table",
		"Output format: table (default) or json")
	return cmd
}

// truncate shortens s to n runes, appending an ellipsis when it had to
// cut. Used by the table renderer so long descriptions don't wrap the
// terminal.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
