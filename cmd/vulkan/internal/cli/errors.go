package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/fang"
	"github.com/jackc/pgx/v5/pgconn"
)

// cliError is the one error type every command returns. It carries the process
// exit code and, for the common case, the message the error handler prints as
// `error: <msg>`. Commands that render their own multi-line failure (register's
// mismatch diff, say) set printed and leave msg empty so nothing double-prints.
type cliError struct {
	code    int
	msg     string
	printed bool
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

// errorHandler is fang's error sink. Everything lands as plain `error: <msg>`
// on stderr -- no styled box -- so the transcripts in ADMIN_CLI.md hold whether
// or not stderr is a TTY, and scripts parsing stderr never branch on styling.
func errorHandler(w io.Writer, _ fang.Styles, err error) {
	var ce *cliError
	if errors.As(err, &ce) {
		if ce.printed || ce.msg == "" {
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

// translateAdminError rewrites raw datastore errors into operator-facing ones.
// The one that matters today: a topic command run before the schema was ever
// migrated hits Postgres 42P01 (undefined_table) deep in a query -- surface the
// fix, not the raw SQLSTATE.
func translateAdminError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return failOp("schema not initialized -- run `vulkan migrate` first")
	}
	return failOp("%s", err.Error())
}
