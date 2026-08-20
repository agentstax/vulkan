package diagnostic

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
)

// Recovery states whether an unchanged retry of the operation can succeed.
type Recovery string

const (
	Transient Recovery = "transient" // attempt unchanged -> retry can succeed
	Permanent Recovery = "permanent" // attempt unchanged -> retry cannot succeed
)

// Error is the one error shape:
// - code
// - recovery
// - problem
// - fix fixed at declaration
// - values
// - wrapped cause attached per raise via With and Wrap
type Error struct {
	Code     string
	Recovery Recovery
	Problem  string
	Fix      string // "" when the code cannot know the remedy
	values   []slog.Attr
	wrapped  error
}

// NewError declares an error condition and registers its code. Declaration
// happens at package init, so structural mistakes panic
// instead of returning an error nothing would check.
func NewError(code string, recovery Recovery, problem string, fix string) *Error {
	if recovery != Transient && recovery != Permanent {
		panic("recovery must be Transient or Permanent: " + string(recovery))
	}
	if problem == "" {
		panic("problem must not be empty: " + code)
	}

	declared := &Error{Code: code, Recovery: recovery, Problem: problem, Fix: fix}
	register(declared)
	return declared
}

// With returns a copy carrying the given name/value pairs appended to any
// already attached.
// Identifier strings render quoted, everything else via its slog value.
func (e *Error) With(pairs ...any) *Error {
	copied := *e
	copied.values = append(slices.Clone(e.values), toAttrs(pairs)...)
	return &copied
}

// Wrap returns a copy carrying cause as the wrapped error, reachable through
// errors.Is/As and rendered after the code in the one-liner.
func (e *Error) Wrap(cause error) *Error {
	copied := *e
	copied.wrapped = cause
	return &copied
}

// Values returns the attached name/value pairs in attachment order.
func (e *Error) Values() []slog.Attr {
	return slices.Clone(e.values)
}

func (e *Error) Unwrap() error {
	return e.wrapped
}

// Error renders the one-liner:
// - problem: name value, name value -- fix [code]: cause.
// No values drops the ":", an empty fix drops the "--",
// no cause drops the trailing chain.
func (e *Error) Error() string {
	var builder strings.Builder
	builder.WriteString(e.Problem)

	for i, attr := range e.values {
		if i == 0 {
			builder.WriteString(": ")
		} else {
			builder.WriteString(", ")
		}
		builder.WriteString(attr.Key)
		builder.WriteString(" ")
		builder.WriteString(formatValue(attr.Value))
	}

	if e.Fix != "" {
		builder.WriteString(" -- ")
		builder.WriteString(e.Fix)
	}

	builder.WriteString(" [")
	builder.WriteString(e.Code)
	builder.WriteString("]")

	if e.wrapped != nil {
		builder.WriteString(": ")
		builder.WriteString(e.wrapped.Error())
	}

	return builder.String()
}

// Is matches on code, so errors.Is(raised, pkg.ErrX) holds for every copy
// With and Wrap produce.
func (e *Error) Is(target error) bool {
	targetError, ok := target.(*Error)
	if !ok {
		return false
	}
	return targetError.Code == e.Code
}

// LogValue renders the same parts as fields for JSON logs.
func (e *Error) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("code", e.Code),
		slog.String("problem", e.Problem),
		slog.String("recovery", string(e.Recovery)),
		slog.String("docs", e.Docs()),
	}

	if e.Fix != "" {
		attrs = append(attrs, slog.String("fix", e.Fix))
	}
	attrs = append(attrs, e.values...)
	if e.wrapped != nil {
		attrs = append(attrs, slog.String("cause", e.wrapped.Error()))
	}

	return slog.GroupValue(attrs...)
}

// Docs returns the error's documentation page, derived from the code.
func (e *Error) Docs() string {
	return docsBaseURL + e.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (e *Error) GetCode() string {
	return e.Code
}

func (e *Error) GetKind() Kind {
	return KindError
}

// Errors lists every registered error ordered by code.
func Errors() []*Error {
	return listRegistered[*Error]()
}

// ***************
// *** HELPERS ***
// ***************

func toAttrs(pairs []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, (len(pairs)+1)/2)
	for i := 0; i < len(pairs); i += 2 {
		name := fmt.Sprint(pairs[i])

		// a name with no value is a raise-site bug; render the gap
		// rather than crash or silently drop the name
		if i+1 >= len(pairs) {
			attrs = append(attrs, slog.String(name, "(missing)"))
			break
		}
		attrs = append(attrs, slog.Any(name, pairs[i+1]))
	}
	return attrs
}

func formatValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return strconv.Quote(value.String())
	}
	return value.String()
}
