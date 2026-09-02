package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
)

func newTopicConfigGetCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name> [key]",
		Short: "Show the topic's config keys: default and current value",
		Example: `  vulkan topic config get orders.created
  vulkan topic config get orders.created retention_ttl`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			key := ""
			if len(args) == 2 {
				key = args[1]
			}
			out := cmd.OutOrStdout()

			entries := topicConfigKeys
			if key != "" {
				entry, ok := findTopicConfigKey(key)
				if !ok {
					return errUnknownTopicConfigKey(key)
				}
				entries = []topicConfigKey{entry}
			}

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			found, err := client.Topic(name).Get(ctx)
			if err != nil {
				return translateAdminError(err)
			}
			if found == nil {
				return errTopicNotFound(name)
			}

			if g.jsonOutput() {
				writeJSON(out, toTopicConfigDocument(found, entries))
				return nil
			}

			fmt.Fprintf(out, "%s topic %q (id=%d)\n", glyphOK(), name, found.Id)
			printTopicConfigLines(out, found, entries)
			return nil
		},
	}

	return cmd
}
