package cli

import (
	"github.com/spf13/cobra"
)

func newAlertCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Inspect published alerts",
	}

	cmd.AddCommand(newAlertListCmd(g))

	return cmd
}
