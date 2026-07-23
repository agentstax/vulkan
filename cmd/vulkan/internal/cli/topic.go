package cli

import (
	"github.com/spf13/cobra"
)

func newTopicCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topic",
		Short: "Register, inspect, and destroy topics",
	}

	cmd.AddCommand(newTopicRegisterCmd(g))
	cmd.AddCommand(newTopicListCmd(g))
	cmd.AddCommand(newTopicGetCmd(g))
	cmd.AddCommand(newTopicDestroyCmd(g))

	return cmd
}

// requireTopicName is the shared Args rule for every single-topic command
// (register/get/destroy): exactly one name, with a verb-specific usage line when
// it's missing so all three fail identically instead of leaking cobra's generic
// "accepts 1 arg(s)" text.
func requireTopicName(verb string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < 1 {
			return failUsage("%s requires a topic name\nusage: vulkan topic %s <name> [flags]", verb, verb)
		}
		if len(args) > 1 {
			return failUsage("%s takes exactly one topic name", verb)
		}
		return nil
	}
}
