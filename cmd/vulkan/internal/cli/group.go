package cli

import (
	"github.com/spf13/cobra"
)

func newGroupCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Inspect and destroy consumer groups",
	}

	cmd.AddCommand(newGroupConfigCmd(g))
	cmd.AddCommand(newGroupDestroyCmd(g))

	return cmd
}
