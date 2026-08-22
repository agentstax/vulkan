package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/spf13/cobra"
)

func newExplainCmd(g *globalFlags) *cobra.Command {
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
				if g.jsonOutput() {
					documents := make([]explainDocument, 0, 64)
					for _, declared := range diagnostic.Errors() {
						documents = append(documents, toErrorExplainDocument(declared, resolvedCliFix(declared)))
					}
					for _, declared := range diagnostic.Events() {
						documents = append(documents, toEventExplainDocument(declared))
					}
					for _, declared := range diagnostic.Metrics() {
						documents = append(documents, toMetricExplainDocument(declared))
					}
					slices.SortFunc(documents, func(left explainDocument, right explainDocument) int {
						return strings.Compare(left.Code, right.Code)
					})
					writeJSON(w, documents)
					return nil
				}

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
				fix := resolvedCliFix(declared)
				if g.jsonOutput() {
					writeJSON(w, toErrorExplainDocument(declared, fix))
					return nil
				}
				renderErrorBlock(w, declared, fix)
				return nil
			}
			for _, declared := range diagnostic.Events() {
				if declared.Code != code {
					continue
				}
				if g.jsonOutput() {
					writeJSON(w, toEventExplainDocument(declared))
					return nil
				}
				renderLogEventBlock(w, declared)
				return nil
			}
			for _, declared := range diagnostic.Metrics() {
				if declared.Code != code {
					continue
				}
				if g.jsonOutput() {
					writeJSON(w, toMetricExplainDocument(declared))
					return nil
				}
				renderMetricBlock(w, declared)
				return nil
			}

			if declared, ok := metricByNameOrAttrKey(args[0]); ok {
				if g.jsonOutput() {
					writeJSON(w, toMetricExplainDocument(declared))
					return nil
				}
				renderMetricBlock(w, declared)
				return nil
			}
			return failOp("unrecognized code or metric: %q -- `vulkan explain` lists every code and metric", args[0])
		},
	}
}

// explainDocument is one declared error, log event, or metric as json; the
// parts a kind doesn't have drop out.
type explainDocument struct {
	Kind        string `json:"kind"` // error | event | metric
	Code        string `json:"code"`
	Problem     string `json:"problem,omitempty"`     // error
	Recovery    string `json:"recovery,omitempty"`    // error
	Fix         string `json:"fix,omitempty"`         // error
	Message     string `json:"message,omitempty"`     // event
	Name        string `json:"name,omitempty"`        // metric
	MetricKind  string `json:"metric_kind,omitempty"` // metric
	Unit        string `json:"unit,omitempty"`        // metric
	Description string `json:"description,omitempty"` // metric
	Docs        string `json:"docs"`
}

// ***************
// *** HELPERS ***
// ***************

func toErrorExplainDocument(declared *diagnostic.Error, fix string) explainDocument {
	return explainDocument{
		Kind:     "error",
		Code:     declared.Code,
		Problem:  declared.Problem,
		Recovery: string(declared.Recovery),
		Fix:      fix,
		Docs:     declared.Docs(),
	}
}

func toEventExplainDocument(declared *diagnostic.Event) explainDocument {
	return explainDocument{
		Kind:    "event",
		Code:    declared.Code,
		Message: declared.Message,
		Docs:    declared.Docs(),
	}
}

func toMetricExplainDocument(declared *diagnostic.Metric) explainDocument {
	return explainDocument{
		Kind:        "metric",
		Code:        declared.Code,
		Name:        declared.Name,
		MetricKind:  declared.Kind,
		Unit:        declared.Unit,
		Description: declared.Description,
		Docs:        declared.Docs(),
	}
}

// resolvedCliFix is a declared error's fix with the CLI rewrite applied when
// cliFixes has one.
func resolvedCliFix(declared *diagnostic.Error) string {
	if cliFix, ok := cliFixes[declared.Code]; ok {
		return cliFix
	}
	return declared.Fix
}

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
