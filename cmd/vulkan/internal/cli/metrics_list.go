package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/metrics"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/spf13/cobra"
)

func newMetricsListCmd(g *globalFlags) *cobra.Command {
	var (
		quiet  bool
		system bool
		user   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the current measurement per (name, attributes) series",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if system && user {
				return failUsage("--system and --user exclude everything together; pass one or neither")
			}
			if quiet && g.jsonOutput() {
				return failUsage("--quiet and --output json cannot be combined")
			}

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			heads, err := client.System().Measurements(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			filtered := make([]*vulkan.Message[metrics.Measurement], 0, len(heads))
			for _, head := range heads {
				fromCollector := strings.HasPrefix(head.Message.Name, metrics.MetricNameReservedPrefix)
				if system && !fromCollector {
					continue
				}
				if user && fromCollector {
					continue
				}
				filtered = append(filtered, head)
			}

			if g.jsonOutput() {
				writeJSON(out, filtered)
				return nil
			}

			if quiet {
				printMeasurementKeys(out, filtered)
			} else {
				printMeasurementsTable(out, filtered)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "series keys only, one per line (for scripts)")
	f.BoolVar(&system, "system", false, "only Vulkan's own metrics (names starting vulkan.)")
	f.BoolVar(&user, "user", false, "only user-produced measurements")
	return cmd
}

func printMeasurementKeys(w io.Writer, heads []*vulkan.Message[metrics.Measurement]) {
	for _, head := range heads {
		fmt.Fprintln(w, head.MessageKey)
	}
}

func printMeasurementsTable(w io.Writer, heads []*vulkan.Message[metrics.Measurement]) {
	if len(heads) == 0 {
		fmt.Fprintln(w, "no measurements published")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tVALUE\tATTRIBUTES\tAT")
	for _, head := range heads {
		measurement := head.Message
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			measurement.Name, measurement.Kind, measurementValueCell(measurement),
			measurementAttributesCell(measurement.Attributes), timeCell(measurement.At.Local()))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%d series\n", len(heads))
}
