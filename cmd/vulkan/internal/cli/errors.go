package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/jackc/pgx/v5/pgconn"
)

// cliFixes rewrites a code's fix into a vulkan command that runs verbatim as
// pasted; codes absent here keep the library's Go-API fix.
var cliFixes = map[string]string{
	// VK0017 system not registered
	"VK0017": "run `vulkan migrate init`",
	// VK0066 compaction head not found
	"VK0066": "run `vulkan topic key messages {topic} {message_key}` to see what was produced under the key",
}

// cliError is the one error type every command returns. It carries the process
// exit code and, for the common case, the message the error handler prints as
// `error: <msg>`. Commands that render their own multi-line failure (register's
// mismatch diff, say) set printed and leave msg empty so nothing double-prints.
type cliError struct {
	code       int
	msg        string
	printed    bool
	structured *diagnostic.DiagnosticError // set when the failure carries the full anatomy; renders as the block
	fix        string                      // the fix line for structured; "" drops it
}

func (e *cliError) Error() string { return e.msg }

// failUsage - bad flags/args/URL. Exit 2.
func failUsage(format string, args ...any) error {
	return &cliError{code: 2, msg: fmt.Sprintf(format, args...)}
}

// failOp - the operation ran and didn't get what the caller wanted (not found,
// not empty, config mismatch, aborted). Exit 1.
func failOp(format string, args ...any) error {
	return &cliError{code: 1, msg: fmt.Sprintf(format, args...)}
}

// failPrinted - the command already wrote its own failure output; the handler
// prints nothing, only the exit code (1) is carried.
func failPrinted() error {
	return &cliError{code: 1, printed: true}
}

// failStructured - the operation surfaced a structured error; the handler
// renders it as the block. fix is the resolved fix line ("" drops it).
func failStructured(structuredError *diagnostic.DiagnosticError, fix string) error {
	return &cliError{code: 1, structured: structuredError, fix: fix}
}

// errorObject is the json rendering of one failure: the LogValue fields of a
// structured error. A plain error fills only Problem; absent parts drop out.
type errorObject struct {
	Code     string         `json:"code,omitempty"`
	Problem  string         `json:"problem"`
	Recovery string         `json:"recovery,omitempty"`
	Values   map[string]any `json:"values,omitempty"`
	Cause    string         `json:"cause,omitempty"`
	Fix      string         `json:"fix,omitempty"`
	Docs     string         `json:"docs,omitempty"`
}

// errorDocument wraps errorObject under the "error" key, so a failure
// document can never be mistaken for a result document.
type errorDocument struct {
	Error errorObject `json:"error"`
}

// errorHandler is fang's error sink. In text mode a structured error renders
// as the block and everything else lands as plain `error: <msg>` -- no styled
// box either way -- so stderr reads the same whether or not it is a TTY, and
// scripts parsing it never branch on styling. In json mode every failure is
// one json document on stderr; a plain error carries only its problem.
func errorHandler(w io.Writer, g *globalFlags, err error) {
	var ce *cliError
	if errors.As(err, &ce) {
		if ce.printed {
			return
		}
		if ce.structured != nil {
			if g.jsonOutput() {
				writeJSON(w, toErrorDocument(ce.structured, ce.fix))
				return
			}
			renderErrorBlock(w, ce.structured, ce.fix)
			return
		}
		if ce.msg == "" {
			return
		}
		if g.jsonOutput() {
			writeJSON(w, toPlainErrorDocument(ce.msg))
			return
		}
		fmt.Fprintf(w, "error: %s\n", ce.msg)
		return
	}

	// cobra's own parse/validation errors (unknown flag, missing arg).
	if g.jsonOutput() {
		writeJSON(w, toPlainErrorDocument(err.Error()))
		return
	}
	fmt.Fprintf(w, "error: %s\n", err.Error())
}

// exitCode maps a returned error to a process exit status: cliError carries its
// own; anything else is a cobra usage/parse error (exit 2).
func exitCode(err error) int {
	var ce *cliError
	if errors.As(err, &ce) {
		if ce.code != 0 {
			return ce.code
		}
		return 1
	}
	return 2
}

// translateAdminError rewrites raw library errors into operator-facing ones:
// a structured error becomes the block (fix swapped for a pasteable vulkan
// command when cliFixes has one, then filled from the raise's own values);
// a topic command run before the system was
// ever migrated hits Postgres 42P01 (undefined_table) deep in a query --
// surface the fix, not the raw SQLSTATE.
func translateAdminError(err error) error {
	if structuredError, ok := errors.AsType[*diagnostic.DiagnosticError](err); ok {
		fix := structuredError.Fix
		if cliFix, ok := cliFixes[structuredError.Code]; ok {
			fix = cliFix
		}
		return failStructured(structuredError, structuredError.Fill(fix))
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return failOp("system not initialized -- run `vulkan migrate init` first")
	}
	return failOp("%s", err.Error())
}

// ***************
// *** HELPERS ***
// ***************

// renderErrorBlock is the CLI's one renderer for a structured error: the
// header line, then one aligned label per fact -- values, cause, the retry
// line when an unchanged retry can succeed, fix, docs.
func renderErrorBlock(w io.Writer, structuredError *diagnostic.DiagnosticError, fix string) {
	fmt.Fprintf(w, "error[%s]: %s\n", structuredError.Code, structuredError.Problem)

	rows := make([][2]string, 0, 8)
	for _, attribute := range structuredError.Values() {
		rows = append(rows, [2]string{attribute.Key, formatAttributeValue(attribute.Value)})
	}
	if cause := structuredError.Unwrap(); cause != nil {
		rows = append(rows, [2]string{"cause", cause.Error()})
	}
	if structuredError.Recovery == diagnostic.RecoveryTransient {
		rows = append(rows, [2]string{"retry", "safe -- an unchanged retry can succeed"})
	}
	if fix != "" {
		rows = append(rows, [2]string{"fix", fix})
	}
	rows = append(rows, [2]string{"docs", structuredError.Docs()})

	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		fmt.Fprintf(w, "  %-*s %s\n", width+1, row[0]+":", row[1])
	}
}

// renderLogEventBlock is renderErrorBlock's sibling for a declared log
// event: the header line, then the docs row.
func renderLogEventBlock(w io.Writer, event *diagnostic.DiagnosticEvent) {
	fmt.Fprintf(w, "event[%s]: %s\n", event.Code, event.Message)
	fmt.Fprintf(w, "  docs: %s\n", event.Docs())
}

// renderMetricBlock is renderErrorBlock's sibling for a declared metric:
// the header line, then kind, unit, scope, attribute keys, description, and
// docs -- an empty unit drops its row.
func renderAlertBlock(w io.Writer, declared *diagnostic.DiagnosticAlert) {
	fmt.Fprintf(w, "alert[%s]: %s\n", declared.Code, declared.Name)

	rows := [][2]string{
		{"severity", declared.Severity},
		{"scope", string(declared.Scope)},
		{"description", declared.Description},
		{"docs", declared.Docs()},
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		fmt.Fprintf(w, "  %-*s %s\n", width+1, row[0]+":", row[1])
	}
}

func renderMetricBlock(w io.Writer, metric *diagnostic.DiagnosticMetric) {
	fmt.Fprintf(w, "metric[%s]: %s\n", metric.Code, metric.Name)

	rows := make([][2]string, 0, 6)
	rows = append(rows, [2]string{"kind", metric.Kind})
	if metric.Unit != "" {
		rows = append(rows, [2]string{"unit", metric.Unit})
	}
	rows = append(rows, [2]string{"scope", string(metric.Scope)})
	attributeKeys := "none"
	if len(metric.AttributeKeys) > 0 {
		attributeKeys = strings.Join(metric.AttributeKeys, ", ")
	}
	rows = append(rows, [2]string{"attribute keys", attributeKeys})
	rows = append(rows, [2]string{"description", metric.Description})
	rows = append(rows, [2]string{"docs", metric.Docs()})

	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		fmt.Fprintf(w, "  %-*s %s\n", width+1, row[0]+":", row[1])
	}
}

// toErrorDocument is renderErrorBlock's json sibling: the same parts as one
// document. fix is the resolved fix line ("" drops it).
func toErrorDocument(structuredError *diagnostic.DiagnosticError, fix string) errorDocument {
	object := errorObject{
		Code:     structuredError.Code,
		Problem:  structuredError.Problem,
		Recovery: string(structuredError.Recovery),
		Fix:      fix,
		Docs:     structuredError.Docs(),
	}

	attributes := structuredError.Values()
	if len(attributes) > 0 {
		object.Values = make(map[string]any, len(attributes))
		for _, attribute := range attributes {
			object.Values[attribute.Key] = jsonAttributeValue(attribute.Value)
		}
	}

	if cause := structuredError.Unwrap(); cause != nil {
		object.Cause = cause.Error()
	}
	return errorDocument{Error: object}
}

// toPlainErrorDocument carries a message-only failure: no code, no recovery,
// no docs -- those parts are unknown, and the exit code stays the usage-vs-op
// discriminator.
func toPlainErrorDocument(message string) errorDocument {
	return errorDocument{Error: errorObject{Problem: message}}
}

func formatAttributeValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return strconv.Quote(value.String())
	}
	return value.String()
}
