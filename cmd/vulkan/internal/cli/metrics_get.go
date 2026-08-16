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

func newMetricsGetCmd(g *globalFlags) *cobra.Command {
	var (
		attributes []string
		limit      int
		series     int
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show a metric's history, one block per attribute set",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return failUsage("get requires a metric name\nusage: vulkan metrics get <name> [flags]")
			}
			if len(args) > 1 {
				return failUsage("get takes exactly one metric name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			if limit <= 0 {
				return failUsage("--limit must be > 0, got %d", limit)
			}
			if series <= 0 {
				return failUsage("--series must be > 0, got %d", series)
			}
			attributeFilter, err := parseAttributePairs(attributes)
			if err != nil {
				return err
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

			matched := make([]*producer.MessageRow[metrics.Sample], 0, len(heads))
			for _, head := range heads {
				if head.Message.Name != name {
					continue
				}
				if !attributesMatch(head.Message.Attributes, attributeFilter) {
					continue
				}
				matched = append(matched, head)
			}

			if len(matched) == 0 {
				fmt.Fprintf(out, "%s no samples published under %q\n", glyphNo(), name)
				return failPrinted()
			}

			fmt.Fprintf(out, "%s metric %q (%s)\n", glyphOK(), name, sampleKindUnitCell(matched[0].Message))

			shown := matched
			if len(shown) > series {
				shown = shown[:series]
			}
			for _, head := range shown {
				messages, err := mAdmin.ListSampleMessages(ctx, head.CompactionKey, limit)
				if err != nil {
					return translateAdminError(err)
				}
				printSampleSeries(out, head.Message.Attributes, messages)
			}

			if len(matched) > series {
				fmt.Fprintf(out, "\nshowing %d of %d series -- narrow with --attribute or raise --series\n", series, len(matched))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&attributes, "attribute", nil, "key=value a series must carry; repeatable, all must match")
	f.IntVar(&limit, "limit", 10, "how many of the newest samples each series lists")
	f.IntVar(&series, "series", 10, "how many attribute sets to list before truncating")
	return cmd
}

// parseAttributePairs turns repeated key=value flags into one filter map.
func parseAttributePairs(pairs []string) (map[string]string, error) {
	parsed := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, failUsage("--attribute takes key=value, got %q", pair)
		}
		parsed[key] = value
	}
	return parsed, nil
}

func attributesMatch(attributes map[string]string, filter map[string]string) bool {
	for key, want := range filter {
		if attributes[key] != want {
			return false
		}
	}
	return true
}

// sampleKindUnitCell - "gauge, {message}"; just "gauge" with no unit.
func sampleKindUnitCell(sample *metrics.Sample) string {
	if sample.Unit == "" {
		return string(sample.Kind)
	}
	return fmt.Sprintf("%s, %s", sample.Kind, sample.Unit)
}

// printSampleSeries is one attribute set's block, newest sample first --
// samples older than the retention window are gone.
func printSampleSeries(w io.Writer, attributes map[string]string, messages []*producer.MessageRow[metrics.Sample]) {
	fmt.Fprintf(w, "\n  %s\n", seriesHeading(attributes))
	if len(messages) == 0 {
		fmt.Fprintln(w, "  no samples in the retention window")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  AT\tVALUE")
	for _, message := range messages {
		fmt.Fprintf(tw, "  %s\t%s\n", timeCell(message.Message.At.Local()), sampleValueCell(message.Message))
	}
	tw.Flush()
}

func seriesHeading(attributes map[string]string) string {
	if len(attributes) == 0 {
		return "(no attributes)"
	}
	return sampleAttributesCell(attributes)
}
