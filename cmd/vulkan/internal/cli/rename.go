package cli

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicRenameCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <name> <new-name>",
		Short: "Change a topic's name (id, config, and messages are untouched)",
		Long: "Rename a topic. Everything but the name -- id, config, stored messages --\n" +
			"is untouched, since tables are addressed by id internally.\n\n" +
			"The old name is free the moment this returns. Running producers/consumers\n" +
			"keep working (they resolved the id at their Register), but anything still\n" +
			"configured with the old name fails its next restart -- or silently attaches\n" +
			"to a new topic later registered under the freed name. Update those configs.",
		Example: "vulkan topic rename orders.created orders.v2",
		Args:    requireRenameArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			oldName, newName := args[0], args[1]
			out := cmd.OutOrStdout()

			if newName == oldName {
				return failUsage("new name matches the current name -- nothing to rename")
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			renamed, err := mAdmin.RenameTopic(ctx, oldName, newName)
			if err != nil {
				switch {
				case errors.Is(err, topic.ErrTopicNotFound):
					return errTopicNotFound(oldName)
				case errors.Is(err, topic.ErrTopicNameTaken):
					return failOp("topic %q already exists -- pick a name that's free, or destroy it first", newName)
				default:
					return translateAdminError(err)
				}
			}

			fmt.Fprintf(out, "%s renamed topic %q -> %q (id=%d)\n", glyphOK(), oldName, newName, renamed.Id)
			return nil
		},
	}

	return cmd
}

// requireRenameArgs is rename's Args rule: exactly two names, with a usage line
// naming the right path when either is missing (cobra's generic "accepts 2
// arg(s)" text names neither).
func requireRenameArgs(_ *cobra.Command, args []string) error {
	if len(args) < 2 {
		return failUsage("rename requires a topic name and a new name\nusage: vulkan topic rename <name> <new-name>")
	}
	if len(args) > 2 {
		return failUsage("rename takes exactly two names: <name> <new-name>")
	}
	return nil
}
