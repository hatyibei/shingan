package cli

import (
	"fmt"

	"github.com/hatyibei/shingan/version"
	"github.com/spf13/cobra"
)

// newVersionCmd prints the shingan release this binary was built
// for. The value is the same string plugins compare against via
// `plugin.Manifest.MinShinganVersion`, so users can use this to
// diagnose "plugin too new for binary" failures.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the shingan binary's release version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.Version)
			return nil
		},
	}
}
