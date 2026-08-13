package cli

import (
	"github.com/spf13/cobra"
)

func newAlertCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Inspect published alerts and consumer group bindings",
	}

	cmd.AddCommand(newAlertListCmd(g))
	cmd.AddCommand(newAlertBindingsCmd(g))

	return cmd
}
