package cli

import (
	"github.com/spf13/cobra"
)

func newGroupBindingCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "binding",
		Short: "Read a consumer group's binding declaration",
		Long: `A group's binding set comes from ConsumerConfig.Bindings, declared on every
consumer Register. Changing it means changing that code and redeploying;
these commands only read.

A group that never declared a set receives every message on its topic.`,
	}

	cmd.AddCommand(newGroupBindingListCmd(g))
	cmd.AddCommand(newGroupBindingGetCmd(g))

	return cmd
}
