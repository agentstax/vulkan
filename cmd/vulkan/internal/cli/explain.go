package cli

import (
	"fmt"
	"io"
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
			"error message, plus the diagnose queries when the declaration has\n" +
			"them. A metric also resolves by its full name or by its stop-line\n" +
			"attribute key (ready_count). With no argument it lists every declared\n" +
			"condition, event, and metric.",
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
				renderDiagnoseQueries(w, declared.Queries)
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
				renderDiagnoseQueries(w, declared.Queries)
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

			if declared, ok := metricByNameOrAttributeKey(args[0]); ok {
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

	Queries []explainQuery `json:"queries,omitempty"` // error, event
}

// explainQuery is one declared diagnose query as json. The placeholders travel
// beside the SQL because the declaration already decides what a placeholder
// is -- a reader of this document never parses the SQL to find out.
type explainQuery struct {
	Label        string   `json:"label"`
	Sql          string   `json:"sql"`
	Placeholders []string `json:"placeholders"`
}

// ***************
// *** HELPERS ***
// ***************

func toErrorExplainDocument(declared *diagnostic.DiagnosticError, fix string) explainDocument {
	return explainDocument{
		Kind:     "error",
		Code:     declared.Code,
		Problem:  declared.Problem,
		Recovery: string(declared.Recovery),
		Fix:      fix,
		Docs:     declared.Docs(),
		Queries:  toExplainQueries(declared.Queries),
	}
}

func toEventExplainDocument(declared *diagnostic.DiagnosticEvent) explainDocument {
	return explainDocument{
		Kind:    "event",
		Code:    declared.Code,
		Message: declared.Message,
		Docs:    declared.Docs(),
		Queries: toExplainQueries(declared.Queries),
	}
}

func toMetricExplainDocument(declared *diagnostic.DiagnosticMetric) explainDocument {
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

func toExplainQueries(queries []*diagnostic.DiagnosticQuery) []explainQuery {
	documents := make([]explainQuery, 0, len(queries))
	for _, query := range queries {
		documents = append(documents, explainQuery{
			Label:        query.Label,
			Sql:          query.Sql,
			Placeholders: query.Placeholders(),
		})
	}
	return documents
}

// renderDiagnoseQueries writes a declaration's diagnose queries under its
// block. Only explain renders them -- the error surface stays the tight block
// that points here. Each label is written as a SQL comment so the section
// pastes into psql as it stands, once the placeholder values are filled in.
func renderDiagnoseQueries(w io.Writer, queries []*diagnostic.DiagnosticQuery) {
	if len(queries) == 0 {
		return
	}

	fmt.Fprintf(w, "\ndiagnose:%s\n", diagnoseSubstitution(queries))
	for _, query := range queries {
		fmt.Fprintf(w, "\n  -- %s\n", query.Label)
		for _, line := range strings.Split(query.Sql, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

// diagnoseSubstitution names every value the reader fills in across the whole
// set, so the instruction is read once rather than per query.
func diagnoseSubstitution(queries []*diagnostic.DiagnosticQuery) string {
	names := make([]string, 0, 4)
	for _, query := range queries {
		for _, name := range query.Placeholders() {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " fill in " + strings.Join(names, ", ") + " with your own values"
}

// resolvedCliFix is a declared error's fix with the CLI rewrite applied when
// cliFixes has one.
func resolvedCliFix(declared *diagnostic.DiagnosticError) string {
	if cliFix, ok := cliFixes[declared.Code]; ok {
		return cliFix
	}
	return declared.Fix
}

// metricByNameOrAttributeKey resolves a metric by its full name, or by a
// stop-line counter attribute key: ready_count strips its suffix and matches
// the declared name whose last segment is ready.
func metricByNameOrAttributeKey(argument string) (*diagnostic.DiagnosticMetric, bool) {
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
