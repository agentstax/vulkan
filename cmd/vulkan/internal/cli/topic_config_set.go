package cli

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/spf13/cobra"
)

func newTopicConfigSetCmd(g *globalFlags) *cobra.Command {
	var schemaVersion int64

	cmd := &cobra.Command{
		Use:   "set <name> <key> <value>",
		Short: "Write one config key",
		Long: `Write one config key on a registered topic. The keys are the ones config
get prints; an unknown key errors and lists them.`,
		Example: `  vulkan topic config set orders.created retention_ttl 720h
  vulkan topic config set orders.created delivery_log_mode all`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name, key, value := args[0], args[1], args[2]
			out := cmd.OutOrStdout()

			entry, ok := findTopicConfigKey(key)
			if !ok {
				return errUnknownTopicConfigKey(key)
			}
			cfg := &topiccontroller.AlterTopicConfig{}
			if err := entry.set(cfg, value); err != nil {
				return failUsage("config key %s takes a %s (got %q)", key, entry.value, value)
			}
			// validate up front for a clean usage error (exit 2) instead of the
			// raw wrapped error AlterTopic returns
			if err := cfg.Validate(); err != nil {
				return failUsage("%s", err)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			updated, err := mAdmin.AlterTopic(ctx, name, topic.SchemaVersion(schemaVersion), cfg)
			if err != nil {
				if errors.Is(err, topic.ErrTopicNotFound) {
					return errTopicNotFound(name)
				}
				return translateAdminError(err)
			}

			fmt.Fprintf(out, "%s set %s on topic %q v%d\n", glyphOK(), key, name, updated.SchemaVersion)
			printTopicConfigLines(out, updated, []topicConfigKey{entry})
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic to change")
	return cmd
}

func newTopicConfigUnsetCmd(g *globalFlags) *cobra.Command {
	var schemaVersion int64

	cmd := &cobra.Command{
		Use:   "unset <name> <key>",
		Short: "Return one config key to its default",
		Long: `Return one config key to its default -- the value an unconfigured register
gets, not the value the topic was registered with.`,
		Example: `  vulkan topic config unset orders.created retention_ttl`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name, key := args[0], args[1]
			out := cmd.OutOrStdout()

			entry, ok := findTopicConfigKey(key)
			if !ok {
				return errUnknownTopicConfigKey(key)
			}
			cfg := &topiccontroller.AlterTopicConfig{}
			entry.unset(cfg)

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			updated, err := mAdmin.AlterTopic(ctx, name, topic.SchemaVersion(schemaVersion), cfg)
			if err != nil {
				if errors.Is(err, topic.ErrTopicNotFound) {
					return errTopicNotFound(name)
				}
				return translateAdminError(err)
			}

			fmt.Fprintf(out, "%s unset %s on topic %q v%d\n", glyphOK(), key, name, updated.SchemaVersion)
			printTopicConfigLines(out, updated, []topicConfigKey{entry})
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic to change")
	return cmd
}
