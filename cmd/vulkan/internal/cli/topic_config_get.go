package cli

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicConfigGetCmd(g *globalFlags) *cobra.Command {
	var schemaVersion int64

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

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			found, err := mAdmin.GetTopic(ctx, name, topic.SchemaVersion(schemaVersion))
			if err != nil {
				return translateAdminError(err)
			}
			if found == nil {
				return errTopicNotFound(name)
			}

			fmt.Fprintf(out, "%s topic %q v%d (id=%d)\n", glyphOK(), name, found.SchemaVersion, found.Id)
			printTopicConfigLines(out, found, entries)
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic to read")
	return cmd
}
