package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/spf13/cobra"
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

			metricsController, err := metricscontroller.NewMetricsController(ds, &metricscontroller.ControllerConfig{
				Logger: logger.NewDefaultLogger(os.Stderr, slog.LevelError),
			})
			if err != nil {
				return failOp("%s", err.Error())
			}

			duties, err := metricsController.DutySnapshots(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			printDutiesTable(out, duties)
			return nil
		},
	}
}

func printDutiesTable(w io.Writer, duties []metrics.DutySnapshot) {
	if len(duties) == 0 {
		fmt.Fprintln(w, "no maintenance duties -- duties appear as topics and consumer groups register")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "TOPIC\tDUTY\tGROUP\tRATE\tGATE AGE\tSTATUS")
	overdue, failing := 0, 0
	for _, d := range duties {
		group := d.ConsumerGroup
		if group == "" {
			group = "-"
		}
		// colored glyphs go in the LAST column only: tabwriter counts ANSI
		// escapes as width, so a colored inner cell would skew every column
		// after it
		status := glyphOK() + " ok"
		switch {
		case d.Attempts > 0:
			status = glyphWarn() + fmt.Sprintf(" failing (%d)", d.Attempts)
			failing++
		case d.Overdue:
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
	fmt.Fprintf(w, "\n%d %s, %d overdue, %d failing\n", len(duties), noun, overdue, failing)
}

// compactDuration is the RATE cell: "720h", "1h", "5s" -- collapse to the
// coarsest whole unit, fall back to Go's duration string for odd values.
func compactDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0s"
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// gateAgeCell renders now() - can_run_after for the duty table: negative while
// a claim holds the gate in the future, positive once the duty sits eligible.
// Sub-second ages keep millisecond detail; anything larger rounds to seconds.
func gateAgeCell(d time.Duration) string {
	if d.Abs() < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
