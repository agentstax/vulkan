package cli

import (
	"github.com/spf13/cobra"
)

func newMaintainCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintain",
		Short: "Run and inspect the deployment's maintenance duties",
	}

	cmd.AddCommand(newMaintainRunCmd(g))
	cmd.AddCommand(newMaintainStatusCmd(g))

	return cmd
}
