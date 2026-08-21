package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [code]",
		Short: "Explain a Vulkan error, log-event, or metric code, offline",
		Long: "explain renders a declared error condition, log event, or metric --\n" +
			"problem, recovery, fix, docs link -- from the code on any log line or\n" +
			"error message. A metric also resolves by its full name or by its\n" +
			"stop-line attr key (ready_count). With no argument it lists every\n" +
			"declared condition, event, and metric.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if len(args) == 0 {
				rows := make([][2]string, 0, 64)
				for _, declared := range diagnostic.Errors() {
					rows = append(rows, [2]string{declared.Code, declared.Problem})
				}
				for _, declared := range diagnostic.Events() {
					rows = append(rows, [2]string{declared.Code, declared.Message})
				}
				for _, declared := range diagnostic.Metrics() {
					rows = append(rows, [2]string{declared.Code, declared.Name})
				}
				slices.SortFunc(rows, func(left [2]string, right [2]string) int {
					return strings.Compare(left[0], right[0])
				})
				for _, row := range rows {
					fmt.Fprintf(w, "%s  %s\n", row[0], row[1])
				}
				return nil
			}

			code := strings.ToUpper(args[0])
			for _, declared := range diagnostic.Errors() {
				if declared.Code != code {
					continue
				}
				fix := declared.Fix
				if cliFix, ok := cliFixes[declared.Code]; ok {
					fix = cliFix
				}
				renderErrorBlock(w, declared, fix)
				return nil
			}
			for _, declared := range diagnostic.Events() {
				if declared.Code != code {
					continue
				}
				renderLogEventBlock(w, declared)
				return nil
			}
			for _, declared := range diagnostic.Metrics() {
				if declared.Code != code {
					continue
				}
				renderMetricBlock(w, declared)
				return nil
			}

			if declared, ok := metricByNameOrAttrKey(args[0]); ok {
				renderMetricBlock(w, declared)
				return nil
			}
			return failOp("unrecognized code or metric: %q -- `vulkan explain` lists every code and metric", args[0])
		},
	}
}

// ***************
// *** HELPERS ***
// ***************

// metricByNameOrAttrKey resolves a metric by its full name, or by a
// stop-line counter attr key: ready_count strips its suffix and matches the
// declared name whose last segment is ready.
func metricByNameOrAttrKey(argument string) (*diagnostic.Metric, bool) {
	if declared, ok := diagnostic.GetMetric(argument); ok {
		return declared, true
	}

	key, ok := strings.CutSuffix(argument, "_count")
	if !ok {
		return nil, false
	}
	for _, declared := range diagnostic.Metrics() {
		if strings.HasSuffix(declared.Name, "."+key) {
			return declared, true
		}
	}
	return nil, false
}
