package cli

import (
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/spf13/cobra"
)

func newTopicKeyMessagesCmd(g *globalFlags) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "messages <topic> <key>",
		Short: "List the key's retained messages, newest first",
		Long: `List the messages the topic still holds under the key, newest first. A RANK
of 0 is a message that never opted into compaction.`,
		Example: `  vulkan topic key messages orders.created order-42 --limit 5`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, messageKey := args[0], args[1]
			out := cmd.OutOrStdout()

			if limit <= 0 {
				return failUsage("--limit must be > 0, got %d", limit)
			}

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			messages, err := client.Topic[vulkan.RawPayload](topicName).Key(messageKey).Messages(ctx, limit)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				if messages == nil {
					messages = make([]*vulkan.StoredMessage[vulkan.RawPayload], 0)
				}
				writeJSON(out, messages)
				return nil
			}

			printKeyMessagesTable(out, topicName, messageKey, messages)
			return nil
		},
	}

	f := cmd.Flags()
	f.IntVar(&limit, "limit", 20, "how many of the newest messages to list")
	return cmd
}

func printKeyMessagesTable(w io.Writer, topicName string, messageKey string, messages []*vulkan.StoredMessage[vulkan.RawPayload]) {
	if len(messages) == 0 {
		fmt.Fprintf(w, "no messages under %q on %q\n", messageKey, topicName)
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "MESSAGE_ID\tCREATED\tRANK\tMESSAGE")
	for _, message := range messages {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n", message.Id, timeCell(message.CreatedAt), message.CompactionRank, compactPayload(*message.Message))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(messages), "message"))
}
