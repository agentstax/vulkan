package cli

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/spf13/cobra"
)

func newScheduleDestroyCmd(g *globalFlags) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Permanently delete a schedule",
		Long: "Permanently delete a schedule. A message already produced and not yet\n" +
			"consumed is not retracted -- destroy stops future requests only.",
		Args: requireScheduleName("destroy"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			// the confirmation prompt would pollute the json document stream
			if g.jsonOutput() && !yes {
				return failUsage("refusing to destroy %q without confirmation -- pass --yes with --output json", name)
			}

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			// Check order matters: a doomed call must never waste a prompt.
			found, err := client.Schedule(name).Get(ctx)
			if err != nil {
				return translateAdminError(err)
			}
			if found == nil {
				return errScheduleNotFound(name)
			}

			// confirm, unless --yes.
			if !yes {
				if !stdinIsTTY() {
					return failUsage("refusing to destroy %q without confirmation -- pass --yes in non-interactive contexts (e.g. CI)", name)
				}
				fmt.Fprintf(out, "This will PERMANENTLY delete schedule %q (id=%d, expression %q).\n", name, found.Id, found.Expression)
				fmt.Fprintln(out, "This cannot be undone.")
				fmt.Fprintln(out)
				fmt.Fprint(out, "Type the schedule name to confirm: ")

				typed, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(typed) != name {
					// No retry loop -- a piped wrong answer gets one shot, then out.
					fmt.Fprintln(out, "aborted: input did not match schedule name")
					return failPrinted()
				}
			}

			if !g.jsonOutput() {
				fmt.Fprintf(out, "destroying %q... ", name)
			}
			if err := client.Schedule(name).Destroy(ctx); err != nil {
				if !g.jsonOutput() {
					fmt.Fprintln(out) // end the dangling "destroying..." line
				}
				if errors.Is(err, schedule.ErrScheduleNotFound) {
					// dropped between our check and the delete
					return errScheduleNotFound(name)
				}
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, scheduleDestroyedDocument{Schedule: name, ScheduleId: found.Id, Destroyed: true})
				return nil
			}
			fmt.Fprintln(out, "done")
			fmt.Fprintf(out, "%s schedule %q destroyed\n", glyphOK(), name)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation (for non-interactive/CI use)")
	return cmd
}

// scheduleDestroyedDocument is schedule destroy's json result: a small
// what-happened record, never the dead row.
type scheduleDestroyedDocument struct {
	Schedule   string `json:"schedule"`
	ScheduleId int64  `json:"schedule_id"`
	Destroyed  bool   `json:"destroyed"`
}
