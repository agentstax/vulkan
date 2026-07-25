package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/maintain/metrics"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/metric/noop"
)

func newMaintainStatusCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List every maintenance duty and flag the overdue ones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			ds, closeDS, err := openDatastore(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeDS()

			metricsDatastore, err := metrics.NewMaintenanceDatastore(ds, &metrics.MaintenanceMetricsDatastoreConfig{
				Logger: logger.NewDefaultLogger(os.Stderr, slog.LevelError),
			})
			if err != nil {
				return failOp("%s", err.Error())
			}
			// DutyState's gauges want a meter; a noop one keeps this one-shot
			// read exporter-free -- Snapshot queries live either way
			state, err := metrics.NewDutyState(noop.NewMeterProvider().Meter("vulkan-cli"), metricsDatastore)
			if err != nil {
				return failOp("%s", err.Error())
			}

			duties, err := state.Snapshot(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			printDutiesTable(out, duties)
			return nil
		},
	}
}

func printDutiesTable(w io.Writer, duties []metrics.DutyStatus) {
	if len(duties) == 0 {
		fmt.Fprintln(w, "no maintenance duties -- duties appear as topics and consumer groups register")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "TOPIC\tDUTY\tGROUP\tRATE\tGATE AGE\tSTATUS")
	overdue := 0
	for _, d := range duties {
		group := d.ConsumerGroup
		if group == "" {
			group = "-"
		}
		// colored glyphs go in the LAST column only: tabwriter counts ANSI
		// escapes as width, so a colored inner cell would skew every column
		// after it
		status := glyphOK() + " ok"
		if d.Overdue {
			status = glyphWarn() + " overdue"
			overdue++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.TopicName, d.Duty, group,
			compactDuration(d.Rate), gateAgeCell(d.GateAge), status,
		)
	}
	tw.Flush()

	noun := "duties"
	if len(duties) == 1 {
		noun = "duty"
	}
	fmt.Fprintf(w, "\n%d %s, %d overdue\n", len(duties), noun, overdue)
}
