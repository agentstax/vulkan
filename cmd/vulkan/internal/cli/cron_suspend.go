package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronSuspendCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "suspend <name>",
		Short: "Stop the scheduler producing a cron job until unsuspended",
		Long: "Stop the scheduler producing a cron job until unsuspended. A job request already produced\n" +
			"and not yet consumed is not retracted -- suspend stops future requests only.",
		Args: requireCronJobName("suspend"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			if err := mAdmin.SuspendCronJob(ctx, name); err != nil {
				if errors.Is(err, cron.ErrCronJobNotFound) {
					return errCronJobNotFound(name)
				}
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(cmd.OutOrStdout(), cronJobSuspendedDocument{CronJob: name, Suspended: true})
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s cron job %q suspended\n", glyphOK(), name)
			return nil
		},
	}
}

func newCronUnsuspendCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unsuspend <name>",
		Short: "Resume a suspended cron job at its schedule's next scheduled time",
		Long: "Resume a suspended cron job at its schedule's next scheduled time from now --\n" +
			"a scheduled time that came due while suspended is dropped, not produced late.",
		Args: requireCronJobName("unsuspend"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			if err := mAdmin.UnsuspendCronJob(ctx, name); err != nil {
				if errors.Is(err, cron.ErrCronJobNotFound) {
					return errCronJobNotFound(name)
				}
				return translateAdminError(err)
			}

			job, err := mAdmin.GetCronJob(ctx, name)
			if err != nil || job == nil {
				// the unsuspend itself succeeded -- report that even if the
				// follow-up read for the next-scheduled-time detail didn't cooperate
				if g.jsonOutput() {
					writeJSON(cmd.OutOrStdout(), cronJobSuspendedDocument{CronJob: name, Suspended: false})
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s cron job %q unsuspended\n", glyphOK(), name)
				return nil
			}

			if g.jsonOutput() {
				writeJSON(cmd.OutOrStdout(), cronJobSuspendedDocument{
					CronJob:         name,
					Suspended:       false,
					NextScheduledAt: &job.NextScheduledAt,
				})
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s cron job %q unsuspended, next scheduled time %s\n",
				glyphOK(), name, timeCell(job.NextScheduledAt))
			return nil
		},
	}
}

// cronJobSuspendedDocument is suspend/unsuspend's json result;
// next_scheduled_at is null while suspended, and after an unsuspend whose
// follow-up read did not cooperate.
type cronJobSuspendedDocument struct {
	CronJob         string     `json:"cron_job"`
	Suspended       bool       `json:"suspended"`
	NextScheduledAt *time.Time `json:"next_scheduled_at"`
}
