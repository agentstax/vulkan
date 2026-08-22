package cli

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newGroupDestroyCmd(g *globalFlags) *cobra.Command {
	var (
		schemaVersion int64
		force         bool
		yes           bool
	)

	cmd := &cobra.Command{
		Use:   "destroy <topic> <group>",
		Short: "Permanently delete a consumer group and everything it owns",
		Long: `Permanently delete a consumer group: its cursor, bindings, leases,
delivery rows, group-owned workers, and group-owned cron jobs. The topic and
its messages are untouched.

Refused while a consumer still runs on the group or the group still holds
delivery rows (failures awaiting retry, or dead-letters); --force overrides
both (a running consumer stops when its worker rows vanish, and the delivery
rows are discarded).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			topicName, groupName := args[0], args[1]
			out := cmd.OutOrStdout()

			// the confirmation prompt would pollute the json document stream
			if g.jsonOutput() && !yes {
				return failUsage("refusing to destroy %q without confirmation -- pass --yes with --output json", groupName)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Check order matters: a doomed call must never waste a prompt.
			found, err := mAdmin.GetTopic(ctx, topicName, topic.SchemaVersion(schemaVersion))
			if err != nil {
				return translateAdminError(err)
			}
			if found == nil {
				return errTopicNotFound(topicName)
			}

			if !yes {
				if !stdinIsTTY() {
					return failUsage("refusing to destroy %q without confirmation -- pass --yes in non-interactive contexts (e.g. CI)", groupName)
				}
				if force {
					fmt.Fprintf(out, "%s --force destroys the group even while a consumer runs on it, and discards its delivery rows.\n", glyphWarn())
				}
				fmt.Fprintf(out, "This will PERMANENTLY delete consumer group %q on topic %q v%d.\n", groupName, topicName, schemaVersion)
				fmt.Fprintln(out, "This cannot be undone.")
				fmt.Fprintln(out)
				fmt.Fprint(out, "Type the group name to confirm: ")

				typed, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(typed) != groupName {
					// No retry loop -- a piped wrong answer gets one shot, then out.
					fmt.Fprintln(out, "aborted: input did not match group name")
					return failPrinted()
				}
			}

			if !g.jsonOutput() {
				fmt.Fprintf(out, "destroying %q... ", groupName)
			}
			if err := mAdmin.DestroyGroup(ctx, topicName, topic.SchemaVersion(schemaVersion), groupName, admin.DestroyOptions{Force: force}); err != nil {
				if !g.jsonOutput() {
					fmt.Fprintln(out) // end the dangling "destroying..." line
				}
				return groupDestroyError(topicName, groupName, err)
			}

			if g.jsonOutput() {
				writeJSON(out, groupDestroyedDocument{
					Topic:     topicName,
					Version:   schemaVersion,
					Group:     groupName,
					Destroyed: true,
				})
				return nil
			}
			fmt.Fprintln(out, "done")
			fmt.Fprintf(out, "%s consumer group %q on topic %q destroyed\n", glyphOK(), groupName, topicName)
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&schemaVersion, "schema-version", 1, "which registered version of the topic the group belongs to")
	f.BoolVar(&force, "force", false, "destroy even while a consumer runs on the group or deliveries await an outcome")
	f.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation (for non-interactive/CI use)")
	return cmd
}

// groupDestroyedDocument is group destroy's json result: a small
// what-happened record, never the dead rows.
type groupDestroyedDocument struct {
	Topic     string `json:"topic"`
	Version   int64  `json:"version"`
	Group     string `json:"group"`
	Destroyed bool   `json:"destroyed"`
}

// groupDestroyError maps a DestroyGroup failure to CLI output.
func groupDestroyError(topicName string, groupName string, err error) error {
	switch {
	case errors.Is(err, consumergroup.ErrGroupNotFound):
		return failOp("consumer group %q not found on topic %q", groupName, topicName)
	case errors.Is(err, consumergroup.ErrGroupLive):
		return failOp("consumer group %q still has a live consumer -- stop it, or pass --force to destroy anyway", groupName)
	case errors.Is(err, consumergroup.ErrGroupDeliveriesPending):
		return failOp("consumer group %q still has delivery rows (failures awaiting retry, or dead-letters) -- pass --force to discard them", groupName)
	case errors.Is(err, topic.ErrTopicNotFound):
		return errTopicNotFound(topicName)
	default:
		return translateAdminError(err)
	}
}
