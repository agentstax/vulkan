package cli

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronRunCmd(g *globalFlags) *cobra.Command {
	var concurrency string

	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Produce one job request for a cron job immediately",
		Long: "Produce one job request for a cron job immediately, outside its schedule. The\n" +
			"schedule and next scheduled time are untouched, and a suspended job still runs.\n\n" +
			"The request runs with concurrency 'parallel' regardless of the job's own\n" +
			"policy -- it runs even while a previous request is still being worked. Pass\n" +
			"--concurrency exclusive to run early without overlapping one. A pending job\n" +
			"request no consumer has claimed yet is superseded by it.",
		Args: requireCronJobName("run"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			f := cmd.Flags()

			// Build a sparse config from only the flags that were passed.
			cfg := &admin.RunCronJobConfig{}
			if f.Changed("concurrency") {
				cfg.Concurrency = common.ConcurrencyPolicy(concurrency)
			}

			// Validate up front for a clean usage error (a bad flag value,
			// exit 2) instead of the raw wrapped error the run returns.
			probe := *cfg
			probe.WithDefaults()
			if err := probe.Validate(); err != nil {
				return failUsage("invalid config: %s", err)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			produced, err := mAdmin.RunCronJob(ctx, name, cfg)
			if err != nil {
				if errors.Is(err, cron.ErrCronJobNotFound) {
					return errCronJobNotFound(name)
				}
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(cmd.OutOrStdout(), cronJobRunDocument{CronJob: name, MessageId: produced.Id})
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s produced job request for %q (message id=%d)\n",
				glyphOK(), name, produced.Id)
			return nil
		},
	}

	cmd.Flags().StringVar(&concurrency, "concurrency", "", "whether the request runs while a previous one is still running, overriding the job's own policy: parallel or exclusive (default parallel)")

	return cmd
}

// cronJobRunDocument is cron run's json result: the handle the run produced.
type cronJobRunDocument struct {
	CronJob   string `json:"cron_job"`
	MessageId int64  `json:"message_id"`
}
