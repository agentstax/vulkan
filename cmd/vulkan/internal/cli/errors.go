package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/charmbracelet/fang"
	"github.com/jackc/pgx/v5/pgconn"
)

// cliFixes rewrites a code's fix into a vulkan command that runs verbatim as
// pasted; codes absent here keep the library's Go-API fix.
var cliFixes = map[string]string{
	// VK0017 schema not registered
	"VK0017": "run `vulkan migrate init`",
}

// cliError is the one error type every command returns. It carries the process
// exit code and, for the common case, the message the error handler prints as
// `error: <msg>`. Commands that render their own multi-line failure (register's
// mismatch diff, say) set printed and leave msg empty so nothing double-prints.
type cliError struct {
	code       int
	msg        string
	printed    bool
	structured *common.Error // set when the failure carries the full anatomy; renders as the block
	fix        string        // the fix line for structured; "" drops it
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
func failStructured(structuredError *common.Error, fix string) error {
	return &cliError{code: 1, structured: structuredError, fix: fix}
}

// errorHandler is fang's error sink. A structured error renders as the block;
// everything else lands as plain `error: <msg>` -- no styled box either way --
// so stderr reads the same whether or not it is a TTY, and scripts parsing it
// never branch on styling.
func errorHandler(w io.Writer, _ fang.Styles, err error) {
	var ce *cliError
	if errors.As(err, &ce) {
		if ce.printed {
			return
		}
		if ce.structured != nil {
			renderErrorBlock(w, ce.structured, ce.fix)
			return
		}
		if ce.msg == "" {
			return
		}
		fmt.Fprintf(w, "error: %s\n", ce.msg)
		return
	}
	// cobra's own parse/validation errors (unknown flag, missing arg).
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
// command when cliFixes has one); a topic command run before the schema was
// ever migrated hits Postgres 42P01 (undefined_table) deep in a query --
// surface the fix, not the raw SQLSTATE.
func translateAdminError(err error) error {
	if structuredError, ok := errors.AsType[*common.Error](err); ok {
		fix := structuredError.Fix
		if cliFix, ok := cliFixes[structuredError.Code]; ok {
			fix = cliFix
		}
		return failStructured(structuredError, fix)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return failOp("schema not initialized -- run `vulkan migrate init` first")
	}
	return failOp("%s", err.Error())
}

// ***************
// *** HELPERS ***
// ***************

// renderErrorBlock is the CLI's one renderer for a structured error: the
// header line, then one aligned label per fact -- values, cause, the retry
// line when an unchanged retry can succeed, fix, docs.
func renderErrorBlock(w io.Writer, structuredError *common.Error, fix string) {
	fmt.Fprintf(w, "error[%s]: %s\n", structuredError.Code, structuredError.Problem)

	rows := make([][2]string, 0, 8)
	for _, attr := range structuredError.Values() {
		rows = append(rows, [2]string{attr.Key, formatAttrValue(attr.Value)})
	}
	if cause := structuredError.Unwrap(); cause != nil {
		rows = append(rows, [2]string{"cause", cause.Error()})
	}
	if structuredError.Recovery == common.Transient {
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

func formatAttrValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return strconv.Quote(value.String())
	}
	return value.String()
}
