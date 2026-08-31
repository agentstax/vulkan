package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/spf13/cobra"
)

func newScheduleSuspendCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "suspend <name>",
		Short: "Stop the scheduler producing a schedule until unsuspended",
		Long: "Stop the schedule producer producing a schedule until unsuspended. A message already produced\n" +
			"and not yet consumed is not retracted -- suspend stops future requests only.",
		Args: requireScheduleName("suspend"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			client, _, closeClient, err := openClient(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeClient()

			if err := client.Schedule(name).Suspend(ctx); err != nil {
				if errors.Is(err, schedule.ErrScheduleNotFound) {
					return errScheduleNotFound(name)
				}
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(cmd.OutOrStdout(), scheduleSuspendedDocument{Schedule: name, Suspended: true})
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s schedule %q suspended\n", glyphOK(), name)
			return nil
		},
	}
}

func newScheduleUnsuspendCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unsuspend <name>",
		Short: "Resume a suspended schedule at its expression's next scheduled time",
		Long: "Resume a suspended schedule at its expression's next scheduled time from now --\n" +
			"a scheduled time that came due while suspended is dropped, not produced late.",
		Args: requireScheduleName("unsuspend"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			client, _, closeClient, err := openClient(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeClient()

			if err := client.Schedule(name).Unsuspend(ctx); err != nil {
				if errors.Is(err, schedule.ErrScheduleNotFound) {
					return errScheduleNotFound(name)
				}
				return translateAdminError(err)
			}

			row, err := client.Schedule(name).Get(ctx)
			if err != nil || row == nil {
				// the unsuspend itself succeeded -- report that even if the
				// follow-up read for the next-scheduled-time detail didn't cooperate
				if g.jsonOutput() {
					writeJSON(cmd.OutOrStdout(), scheduleSuspendedDocument{Schedule: name, Suspended: false})
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s schedule %q unsuspended\n", glyphOK(), name)
				return nil
			}

			if g.jsonOutput() {
				writeJSON(cmd.OutOrStdout(), scheduleSuspendedDocument{
					Schedule:        name,
					Suspended:       false,
					NextScheduledAt: &row.NextScheduledAt,
				})
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s schedule %q unsuspended, next scheduled time %s\n",
				glyphOK(), name, timeCell(row.NextScheduledAt))
			return nil
		},
	}
}

// scheduleSuspendedDocument is suspend/unsuspend's json result;
// next_scheduled_at is null while suspended, and after an unsuspend whose
// follow-up read did not cooperate.
type scheduleSuspendedDocument struct {
	Schedule        string     `json:"schedule"`
	Suspended       bool       `json:"suspended"`
	NextScheduledAt *time.Time `json:"next_scheduled_at"`
}
