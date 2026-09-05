package cli

import (
	"fmt"
	"log/slog"

	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/spf13/cobra"
)

func newGroupBindingGetCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <topic> <group>",
		Short: "Show the group's effective binding set",
		Long: `Show the group's effective binding set -- its newest installed declaration.
A group that never declared a set prints none and receives every message
on its topic.`,
		Example: `  vulkan group binding get orders billing`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, groupName := args[0], args[1]
			out := cmd.OutOrStdout()

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			// Binding().Get collapses an absent group into nil; the command
			// reports absence as not-found like group config get does
			found, err := client.Topic[vulkan.RawPayload](topicName).Get(ctx)
			if err != nil {
				return groupError(topicName, groupName, err)
			}
			if found == nil {
				return errTopicNotFound(topicName)
			}
			group, err := client.Topic[vulkan.RawPayload](topicName).Group(groupName).Get(ctx)
			if err != nil {
				return groupError(topicName, groupName, err)
			}
			if group == nil {
				return failOp("consumer group %q not found on topic %q", groupName, topicName)
			}

			binding, err := client.Topic[vulkan.RawPayload](topicName).Group(groupName).Binding().Get(ctx)
			if err != nil {
				return groupError(topicName, groupName, err)
			}

			if g.jsonOutput() {
				writeJSON(out, binding)
				return nil
			}

			fmt.Fprintf(out, "%s consumer group %q on topic %q\n", glyphOK(), groupName, topicName)
			if binding == nil {
				fmt.Fprintln(out, "  (no binding declared -- the group receives every message on its topic)")
				return nil
			}
			printBindingsTable(out, []*consume.Binding{binding})
			return nil
		},
	}

	return cmd
}
