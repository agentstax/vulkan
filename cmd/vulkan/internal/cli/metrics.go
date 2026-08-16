package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/spf13/cobra"
)

func newMetricsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Inspect published metric samples",
	}

	cmd.AddCommand(newMetricsListCmd(g))
	cmd.AddCommand(newMetricsGetCmd(g))

	return cmd
}

// sampleValueCell renders a value by its UCUM unit: a real unit carries a
// dimension the cell can format ("ms" -> 47s), while a braced annotation is
// only a human label for a dimensionless count, so its number prints bare.
// Real units other than "ms" print verbatim beside the number.
func sampleValueCell(sample *metrics.Sample) string {
	unit := string(sample.Unit)
	switch {
	case sample.Unit == metrics.UnitMilliseconds:
		return time.Duration(sample.Value * float64(time.Millisecond)).Round(time.Millisecond).String()
	case unit == "" || (strings.HasPrefix(unit, "{") && strings.HasSuffix(unit, "}")):
		return sampleNumber(sample.Value)
	default:
		return sampleNumber(sample.Value) + " " + unit
	}
}

// sampleNumber prints 32 as "32", not "32.000000".
func sampleNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// sampleAttributesCell - "group=billing,topic=orders", sorted like SampleKey;
// "-" with no attributes.
func sampleAttributesCell(attributes map[string]string) string {
	if len(attributes) == 0 {
		return "-"
	}

	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, attributes[key]))
	}
	return strings.Join(pairs, ",")
}
