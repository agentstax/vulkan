package cli

import (
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newGroupConfigSetCmd(g *globalFlags) *cobra.Command {
	var schemaVersion int64

	cmd := &cobra.Command{
		Use:   "set <topic> <group> <key> <value>",
		Short: "Write an operator override on one config key",
		Long: `Write an operator override over the default the group's code declares.
The keys are the ones config get prints; an unknown key errors and lists
them. A message field (message.timeout, say) patches that field onto the
group's current effective message document and writes the result as one
override -- the document itself can't be set whole.`,
		Example: `  vulkan group config set orders billing claim_poll_rate 1s
  vulkan group config set orders billing message.timeout 2m`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, groupName, key, value := args[0], args[1], args[2], args[3]
			version := topic.SchemaVersion(schemaVersion)
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			cfg := &admin.AlterGroupConfig{}
			entry, isConfigKey := findGroupConfigKey(key)
			field, isMessageField := findMessageFieldKey(key)
			switch {
			case key == "message":
				return failUsage("the message document is set one field at a time, e.g. message.timeout -- config get lists the fields")
			case isConfigKey:
				if err := entry.set(cfg, value); err != nil {
					return failUsage("config key %s takes a %s (got %q)", key, entry.value, value)
				}
			case isMessageField:
				workers, err := mAdmin.GetGroup(ctx, topicName, version, groupName)
				if err != nil {
					return groupError(topicName, groupName, err)
				}
				options, err := effectiveMessageOptions(workers)
				if err != nil {
					return failOp("%s", err)
				}
				if err := field.patch(options, value); err != nil {
					return failUsage("config key %s takes a %s (got %q)", key, field.value, value)
				}
				cfg.Message = common.Set(*options)
			default:
				return errUnknownGroupConfigKey(key)
			}

			// validate up front for a clean usage error (exit 2) instead of the
			// raw wrapped error AlterGroup returns
			if err := cfg.Validate(); err != nil {
				return failUsage("%s", err)
			}
			if err := mAdmin.AlterGroup(ctx, topicName, version, groupName, cfg); err != nil {
				return groupError(topicName, groupName, err)
			}

			workers, err := mAdmin.GetGroup(ctx, topicName, version, groupName)
			if err != nil {
				return groupError(topicName, groupName, err)
			}
			fmt.Fprintf(out, "%s set %s on consumer group %q on topic %q\n", glyphOK(), key, groupName, topicName)
			// a message field set rewrites the whole document -- show all of it
			filter := key
			if i := strings.IndexByte(key, '.'); i >= 0 {
				filter = key[:i]
			}
			printGroupConfigLines(out, filterGroupConfigLines(groupConfigLines(workers), filter))
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic the group belongs to")
	return cmd
}

func newGroupConfigUnsetCmd(g *globalFlags) *cobra.Command {
	var schemaVersion int64

	cmd := &cobra.Command{
		Use:   "unset <topic> <group> <key>",
		Short: "Remove an operator override, returning the key to its default",
		Long: `Remove the operator override on one config key -- the default the group's
code declares takes effect again. Unsetting a key with no override changes
nothing. The message override is one document, so it unsets whole: unset
message, not one of its fields.`,
		Example: `  vulkan group config unset orders billing claim_poll_rate
  vulkan group config unset orders billing message`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, groupName, key := args[0], args[1], args[2]
			version := topic.SchemaVersion(schemaVersion)
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			cfg := &admin.AlterGroupConfig{}
			entry, isConfigKey := findGroupConfigKey(key)
			_, isMessageField := findMessageFieldKey(key)
			switch {
			case key == "message":
				cfg.Message = common.Unset[common.MessageOptions]()
			case isConfigKey:
				entry.unset(cfg)
			case isMessageField:
				return failUsage("the message override is one document -- unset message removes it whole")
			default:
				return errUnknownGroupConfigKey(key)
			}

			if err := mAdmin.AlterGroup(ctx, topicName, version, groupName, cfg); err != nil {
				return groupError(topicName, groupName, err)
			}

			workers, err := mAdmin.GetGroup(ctx, topicName, version, groupName)
			if err != nil {
				return groupError(topicName, groupName, err)
			}
			fmt.Fprintf(out, "%s unset %s on consumer group %q on topic %q\n", glyphOK(), key, groupName, topicName)
			printGroupConfigLines(out, filterGroupConfigLines(groupConfigLines(workers), key))
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic the group belongs to")
	return cmd
}
