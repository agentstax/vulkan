package cli

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronRunCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Produce one firing of a cron job immediately",
		Long: "Produce one firing of a cron job immediately, outside its schedule. The\n" +
			"schedule and next firing are untouched, and a suspended job still runs.\n\n" +
			"The firing runs with concurrency 'allow' regardless of the job's own\n" +
			"policy -- it runs even while a previous firing is still being worked. A\n" +
			"pending scheduled firing no consumer has claimed yet is superseded by it.",
		Args: requireCronJobName("run"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			produced, err := mAdmin.RunCronJob(ctx, name)
			if err != nil {
				if errors.Is(err, cron.ErrCronJobNotFound) {
					return errCronJobNotFound(name)
				}
				return translateAdminError(err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s produced firing of %q (message id=%d)\n",
				glyphOK(), name, produced.Id)
			return nil
		},
	}
}
