package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/spf13/cobra"
)

func newTopicKeyGetCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <topic> <key>",
		Short: "Show the key's compaction head",
		Long: `Show the key's compaction head -- the message that currently wins under it.
A key nothing was produced under with compaction enabled has no head and
exits non-zero with VK0066.`,
		Example: `  vulkan topic key get orders.created order-42`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, messageKey := args[0], args[1]
			out := cmd.OutOrStdout()

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			head, err := client.Topic[vulkan.RawPayload](topicName).Key(messageKey).CompactionHead(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, head)
				return nil
			}

			fmt.Fprintf(out, "%s compaction head for %q on %q\n\n", glyphOK(), messageKey, topicName)
			printStoredMessage(out, head)
			return nil
		},
	}

	return cmd
}

// printStoredMessage is the row's facts one per line, then the payload
// re-indented under its own label.
func printStoredMessage(w io.Writer, head *vulkan.StoredMessage[vulkan.RawPayload]) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  MessageId\t%d\n", head.Id)
	fmt.Fprintf(tw, "  CreatedAt\t%s\n", timeCell(head.CreatedAt))
	fmt.Fprintf(tw, "  RoutingKey\t%s\n", head.RoutingKey)
	fmt.Fprintf(tw, "  CompactionRank\t%d\n", head.CompactionRank)
	tw.Flush()

	fmt.Fprintln(w, "  Message")
	fmt.Fprintln(w, indentedPayload(*head.Message, "    "))
}

// indentedPayload re-indents the stored JSON under prefix; bytes that do not
// indent (they always should -- the column is JSONB) print as they are.
func indentedPayload(payload vulkan.RawPayload, prefix string) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, payload, prefix, "  "); err != nil {
		return prefix + string(payload)
	}
	return prefix + indented.String()
}

// compactPayload is the stored JSON on one line, for table cells.
func compactPayload(payload vulkan.RawPayload) string {
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, payload); err != nil {
		return string(payload)
	}
	return compacted.String()
}
