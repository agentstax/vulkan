package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
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
		Short: "List the current sample per (name, attributes) series",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if system && user {
				return failUsage("--system and --user exclude everything together; pass one or neither")
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			heads, err := mAdmin.ListSamples(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			filtered := make([]*producer.MessageRow[metrics.Sample], 0, len(heads))
			for _, head := range heads {
				fromCollector := strings.HasPrefix(head.Message.Name, "vulkan.")
				if system && !fromCollector {
					continue
				}
				if user && fromCollector {
					continue
				}
				filtered = append(filtered, head)
			}

			if quiet {
				printSampleKeys(out, filtered)
			} else {
				printSamplesTable(out, filtered)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "series keys only, one per line (for scripts)")
	f.BoolVar(&system, "system", false, "only Vulkan's own samples (names starting vulkan.)")
	f.BoolVar(&user, "user", false, "only user-produced samples")
	return cmd
}

func printSampleKeys(w io.Writer, heads []*producer.MessageRow[metrics.Sample]) {
	for _, head := range heads {
		fmt.Fprintln(w, head.CompactionKey)
	}
}

func printSamplesTable(w io.Writer, heads []*producer.MessageRow[metrics.Sample]) {
	if len(heads) == 0 {
		fmt.Fprintln(w, "no samples published")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tVALUE\tATTRIBUTES\tAT")
	for _, head := range heads {
		sample := head.Message
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			sample.Name, sample.Kind, sampleValueCell(sample),
			sampleAttributesCell(sample.Attributes), timeCell(sample.At.Local()))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(heads), "sample"))
}
