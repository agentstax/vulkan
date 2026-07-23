package cli

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicDestroyCmd(g *globalFlags) *cobra.Command {
	var (
		force bool
		yes   bool
	)

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Permanently delete a topic and every message it holds",
		Args:  requireTopicName("destroy"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			mAdmin, ds, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Check order matters: a doomed call must never waste a prompt.
			// 1. exists?
			found, err := mAdmin.GetTopic(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}
			if found == nil {
				return errTopicNotFound(name)
			}

			// 2. emptiness -- MessageAdmin doesn't expose this, so build a
			// topic.TopicDatastore over the same pool (public API, no pkg change).
			tds, err := topic.NewTopicDatastore(ds, logger.NewDefaultLogger(os.Stderr, slog.LevelError), nil)
			if err != nil {
				return failOp("could not check whether topic is empty: %v", err)
			}
			empty, err := tds.IsEmpty(ctx, found.Id)
			if err != nil {
				return translateAdminError(err)
			}
			if !empty && !force {
				return errTopicNotEmpty(name)
			}

			// 3. confirm, unless --yes.
			if !yes {
				if !stdinIsTTY() {
					return failUsage("refusing to destroy %q without confirmation -- pass --yes in non-interactive contexts (e.g. CI)", name)
				}
				if !empty { // implies --force by the gate above
					fmt.Fprintf(out, "%s topic %q still holds messages -- --force will delete them along with the topic.\n", glyphWarn(), name)
				}
				fmt.Fprintf(out, "This will PERMANENTLY delete topic %q (id=%d) and every message it holds.\n", name, found.Id)
				fmt.Fprintln(out, "This cannot be undone.")
				fmt.Fprintln(out)
				fmt.Fprint(out, "Type the topic name to confirm: ")

				typed, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(typed) != name {
					// No retry loop -- a piped wrong answer gets one shot, then out.
					fmt.Fprintln(out, "aborted: input did not match topic name")
					return failPrinted()
				}
			}

			// 4. destroy.
			fmt.Fprintf(out, "destroying %q... ", name)
			if err := mAdmin.DestroyTopic(ctx, name, admin.DestroyOptions{Force: force}); err != nil {
				fmt.Fprintln(out) // end the dangling "destroying..." line
				return destroyError(name, err)
			}
			fmt.Fprintln(out, "done")
			fmt.Fprintf(out, "%s topic %q destroyed\n", glyphOK(), name)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "required to destroy a topic that still holds messages")
	f.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation (for non-interactive/CI use)")
	return cmd
}

// errTopicNotFound / errTopicNotEmpty are the two operator-facing messages
// destroy raises from more than one place (pre-flight and the post-delete race
// map below). Single source each so the wording can never drift.
func errTopicNotFound(name string) error {
	return failOp("topic %q not found", name)
}

func errTopicNotEmpty(name string) error {
	return failOp("topic %q still holds messages -- pass --force to destroy anyway (this is unrecoverable data loss, not just a schema drop)", name)
}

// destroyError maps a DestroyTopic failure to CLI output. Most cases are caught
// in pre-flight; these are the narrow races (a producer writes, or the topic is
// dropped, between our checks and the delete) plus anything unexpected.
func destroyError(name string, err error) error {
	switch {
	case errors.Is(err, topic.ErrTopicNotEmpty):
		return errTopicNotEmpty(name)
	case errors.Is(err, topic.ErrTopicNotFound):
		return errTopicNotFound(name)
	default:
		return translateAdminError(err)
	}
}
