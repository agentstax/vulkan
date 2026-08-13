package cli

import (
	"github.com/spf13/cobra"
)

func newGroupCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage consumer groups",
	}

	cmd.AddCommand(newGroupDestroyCmd(g))

	return cmd
}
