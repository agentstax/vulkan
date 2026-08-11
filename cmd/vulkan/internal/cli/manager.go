package cli

import (
	"github.com/spf13/cobra"
)

func newManagerCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Run the deployment's system manager",
	}

	cmd.AddCommand(newManagerRunCmd(g))

	return cmd
}
