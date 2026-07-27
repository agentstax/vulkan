package cli

import (
	"github.com/spf13/cobra"
)

func newSystemCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Inspect and alter the singleton system config",
	}

	cmd.AddCommand(newSystemGetCmd(g))
	cmd.AddCommand(newSystemAlterCmd(g))

	return cmd
}
