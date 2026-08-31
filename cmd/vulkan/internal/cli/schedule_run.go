package cli

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/spf13/cobra"
)

func newScheduleRunCmd(g *globalFlags) *cobra.Command {
	var concurrency string

	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Produce a schedule's message immediately",
		Long: "Produce a schedule's stored message immediately, outside its expression. The\n" +
			"expression and next scheduled time are untouched, and a suspended row still runs.\n\n" +
			"The request runs with concurrency 'parallel' regardless of the row's own\n" +
			"policy -- it runs even while a previous request is still being worked. Pass\n" +
			"--concurrency exclusive to run early without overlapping one. A pending row\n" +
			"request no consumer has claimed yet is superseded by it.",
		Args: requireScheduleName("run"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			f := cmd.Flags()

			// Build a sparse config from only the flags that were passed.
			cfg := &vulkan.RunScheduleConfig{}
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

			client, _, closeClient, err := openClient(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeClient()

			produced, err := client.Schedule(name).Run(ctx, cfg)
			if err != nil {
				if errors.Is(err, schedule.ErrScheduleNotFound) {
					return errScheduleNotFound(name)
				}
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(cmd.OutOrStdout(), scheduleRunDocument{Schedule: name, MessageId: produced.Id})
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s produced %q (message id=%d)\n",
				glyphOK(), name, produced.Id)
			return nil
		},
	}

	cmd.Flags().StringVar(&concurrency, "concurrency", "", "whether the request runs while a previous one is still running, overriding the row's own policy: parallel or exclusive (default parallel)")

	return cmd
}

// scheduleRunDocument is schedule run's json result: the handle the run produced.
type scheduleRunDocument struct {
	Schedule  string `json:"schedule"`
	MessageId int64  `json:"message_id"`
}
