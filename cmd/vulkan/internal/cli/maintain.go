package cli

import (
	"github.com/spf13/cobra"
)

func newMaintainCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintain",
		Short: "Run the deployment's maintenance workers",
	}

	cmd.AddCommand(newMaintainRunCmd(g))

	return cmd
}
