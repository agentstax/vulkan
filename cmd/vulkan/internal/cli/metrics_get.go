package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/metrics"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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

			client, _, closeClient, err := openClient(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeClient()

			heads, err := client.ListMeasurements(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			matched := make([]*vulkan.MessageData[metrics.Measurement], 0, len(heads))
			for _, head := range heads {
				if head.Message.Name != name {
					continue
				}
				if !attributesMatch(head.Message.Attributes, attributeFilter) {
					continue
				}
				matched = append(matched, head)
			}

			if g.jsonOutput() {
				document := metricGetDocument{
					Name:        name,
					Exists:      len(matched) > 0,
					Series:      make([]metricSeriesDocument, 0, len(matched)),
					SeriesTotal: len(matched),
				}
				if len(matched) > 0 {
					document.Kind = string(matched[0].Message.Kind)
					document.Unit = string(matched[0].Message.Unit)
				}

				shown := matched
				if len(shown) > series {
					shown = shown[:series]
				}
				for _, head := range shown {
					messages, err := client.ListMeasurementMessages(ctx, head.MessageKey, limit)
					if err != nil {
						return translateAdminError(err)
					}
					if messages == nil {
						messages = make([]*vulkan.MessageData[metrics.Measurement], 0)
					}
					document.Series = append(document.Series, metricSeriesDocument{
						Attributes:   head.Message.Attributes,
						Measurements: messages,
					})
				}

				writeJSON(out, document)
				if len(matched) == 0 {
					return failPrinted()
				}
				return nil
			}

			if len(matched) == 0 {
				fmt.Fprintf(out, "%s no measurements published under %q\n", glyphNo(), name)
				return failPrinted()
			}

			fmt.Fprintf(out, "%s metric %q (%s)\n", glyphOK(), name, measurementKindUnitCell(matched[0].Message))

			shown := matched
			if len(shown) > series {
				shown = shown[:series]
			}
			for _, head := range shown {
				messages, err := client.ListMeasurementMessages(ctx, head.MessageKey, limit)
				if err != nil {
					return translateAdminError(err)
				}
				printMeasurementSeries(out, head.Message.Attributes, messages)
			}

			if len(matched) > series {
				fmt.Fprintf(out, "\nshowing %d of %d series -- narrow with --attribute or raise --series\n", series, len(matched))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&attributes, "attribute", nil, "key=value a series must carry; repeatable, all must match")
	f.IntVar(&limit, "limit", 10, "how many of the newest measurements each series lists")
	f.IntVar(&series, "series", 10, "how many attribute sets to list before truncating")
	return cmd
}

// metricGetDocument is metrics get's json result; the not-found case is data
// (exists false, series empty), the exit code stays 1. SeriesTotal counts
// every matched series before --series truncation.
type metricGetDocument struct {
	Name        string                 `json:"name"`
	Exists      bool                   `json:"exists"`
	Kind        string                 `json:"kind,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	Series      []metricSeriesDocument `json:"series"`
	SeriesTotal int                    `json:"series_total"`
}

// metricSeriesDocument is one attribute set's history, newest first.
type metricSeriesDocument struct {
	Attributes   map[string]string                          `json:"attributes"`
	Measurements []*vulkan.MessageData[metrics.Measurement] `json:"measurements"`
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

// measurementKindUnitCell - "gauge, {message}"; just "gauge" with no unit.
func measurementKindUnitCell(measurement *metrics.Measurement) string {
	if measurement.Unit == "" {
		return string(measurement.Kind)
	}
	return fmt.Sprintf("%s, %s", measurement.Kind, measurement.Unit)
}

// printMeasurementSeries is one attribute set's block, newest measurement first --
// measurements older than the retention window are gone.
func printMeasurementSeries(w io.Writer, attributes map[string]string, messages []*vulkan.MessageData[metrics.Measurement]) {
	fmt.Fprintf(w, "\n  %s\n", seriesHeading(attributes))
	if len(messages) == 0 {
		fmt.Fprintln(w, "  no measurements in the retention window")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  AT\tVALUE")
	for _, message := range messages {
		fmt.Fprintf(tw, "  %s\t%s\n", timeCell(message.Message.At.Local()), measurementValueCell(message.Message))
	}
	tw.Flush()
}

func seriesHeading(attributes map[string]string) string {
	if len(attributes) == 0 {
		return "(no attributes)"
	}
	return measurementAttributesCell(attributes)
}
