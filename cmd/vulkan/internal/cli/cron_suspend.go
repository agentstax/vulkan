package cli

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronSuspendCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "suspend <name>",
		Short: "Stop a cron job from firing until unsuspended",
		Long: "Stop a cron job from firing until unsuspended. A firing already produced\n" +
			"and not yet consumed is not retracted -- suspend stops future firings only.",
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

			fmt.Fprintf(cmd.OutOrStdout(), "%s cron job %q suspended\n", glyphOK(), name)
			return nil
		},
	}
}

func newCronUnsuspendCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unsuspend <name>",
		Short: "Resume a suspended cron job at its schedule's next firing",
		Long: "Resume a suspended cron job at its schedule's next firing from now --\n" +
			"a firing that came due while suspended is dropped, not fired late.",
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
				// follow-up read for the next-firing detail didn't cooperate
				fmt.Fprintf(cmd.OutOrStdout(), "%s cron job %q unsuspended\n", glyphOK(), name)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s cron job %q unsuspended, next firing %s\n",
				glyphOK(), name, timeCell(job.NextScheduledTime))
			return nil
		},
	}
}
