package diagnostic

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
)

// DiagnosticRecovery states whether an unchanged retry of the operation can succeed.
type DiagnosticRecovery string

const (
	RecoveryTransient DiagnosticRecovery = "transient" // attempt unchanged -> retry can succeed
	RecoveryPermanent DiagnosticRecovery = "permanent" // attempt unchanged -> retry cannot succeed
)

// Error is the one error shape:
// - code
// - recovery
// - problem
// - fix
// - diagnose queries fixed at declaration
// - values
// - wrapped cause attached per raise via With and Wrap
type DiagnosticError struct {
	Code     string
	Recovery DiagnosticRecovery
	Problem  string
	Fix      string             // "" when the code cannot know the remedy; may carry {attribute} placeholders
	Queries  []*DiagnosticQuery // none when the condition has no state to look at
	values   []slog.Attr
	wrapped  error
}

// NewDiagnosticError declares an error condition and registers its code. Declaration
// happens at package init, so structural mistakes panic
// instead of returning an error nothing would check.
func NewDiagnosticError(code string, recovery DiagnosticRecovery, problem string, fix string) *DiagnosticError {
	if recovery != RecoveryTransient && recovery != RecoveryPermanent {
		panic("recovery must be RecoveryTransient or RecoveryPermanent: " + string(recovery))
	}
	if problem == "" {
		panic("problem must not be empty: " + code)
	}

	declared := &DiagnosticError{Code: code, Recovery: recovery, Problem: problem, Fix: fix}
	register(declared)
	return declared
}

// Diagnose attaches the queries that show an operator the state behind this
// condition, and returns the same declaration so it chains onto NewDiagnosticError.
func (e *DiagnosticError) Diagnose(queries ...*DiagnosticQuery) *DiagnosticError {
	if len(queries) == 0 {
		panic("diagnose queries must not be empty: " + e.Code)
	}
	if len(e.Queries) > 0 {
		panic("diagnose queries are already declared: " + e.Code)
	}

	e.Queries = queries
	return e
}

// FixPlaceholders lists each attribute name the fix substitutes, once, in
// first-appearance order.
func (e *DiagnosticError) FixPlaceholders() []string {
	return placeholderNames(e.Fix)
}

// Fill substitutes text's {attribute} placeholders with the values this raise
// attached -- the declared fix, or a surface's own rewording of it.
func (e *DiagnosticError) Fill(text string) string {
	return fillPlaceholders(text, e.values)
}

// With returns a copy carrying the given name/value pairs appended to any
// already attached.
// Identifier strings render quoted, everything else via its slog value.
func (e *DiagnosticError) With(pairs ...any) *DiagnosticError {
	copied := *e
	copied.values = append(slices.Clone(e.values), toAttributes(pairs)...)
	return &copied
}

// Wrap returns a copy carrying cause as the wrapped error, reachable through
// errors.Is/As and rendered after the code in the one-liner.
func (e *DiagnosticError) Wrap(cause error) *DiagnosticError {
	copied := *e
	copied.wrapped = cause
	return &copied
}

// Values returns the attached name/value pairs in attachment order.
func (e *DiagnosticError) Values() []slog.Attr {
	return slices.Clone(e.values)
}

func (e *DiagnosticError) Unwrap() error {
	return e.wrapped
}

// Error renders the one-liner:
// - problem: name value, name value -- fix [code]: cause.
// No values drops the ":", an empty fix drops the "--",
// no cause drops the trailing chain.
// The fix's placeholders fill from the attached values.
func (e *DiagnosticError) Error() string {
	var builder strings.Builder
	builder.WriteString(e.Problem)

	for i, attribute := range e.values {
		if i == 0 {
			builder.WriteString(": ")
		} else {
			builder.WriteString(", ")
		}
		builder.WriteString(attribute.Key)
		builder.WriteString(" ")
		builder.WriteString(formatValue(attribute.Value))
	}

	if e.Fix != "" {
		builder.WriteString(" -- ")
		builder.WriteString(e.Fill(e.Fix))
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
func (e *DiagnosticError) Is(target error) bool {
	targetError, ok := target.(*DiagnosticError)
	if !ok {
		return false
	}
	return targetError.Code == e.Code
}

// LogValue renders the same parts as fields for JSON logs.
func (e *DiagnosticError) LogValue() slog.Value {
	attributes := []slog.Attr{
		slog.String("code", e.Code),
		slog.String("problem", e.Problem),
		slog.String("recovery", string(e.Recovery)),
		slog.String("docs", e.Docs()),
	}

	if e.Fix != "" {
		attributes = append(attributes, slog.String("fix", e.Fill(e.Fix)))
	}
	attributes = append(attributes, e.values...)
	if e.wrapped != nil {
		attributes = append(attributes, slog.String("cause", e.wrapped.Error()))
	}

	return slog.GroupValue(attributes...)
}

// Docs returns the error's documentation page, derived from the code.
func (e *DiagnosticError) Docs() string {
	return docsBaseURL + e.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (e *DiagnosticError) GetCode() string {
	return e.Code
}

func (e *DiagnosticError) GetKind() DiagnosticKind {
	return DiagnosticKindError
}

// Errors lists every registered error ordered by code.
func Errors() []*DiagnosticError {
	return listRegistered[*DiagnosticError]()
}

// ***************
// *** HELPERS ***
// ***************

func toAttributes(pairs []any) []slog.Attr {
	attributes := make([]slog.Attr, 0, (len(pairs)+1)/2)
	for i := 0; i < len(pairs); i += 2 {
		name := fmt.Sprint(pairs[i])

		// a name with no value is a raise-site bug; render the gap
		// rather than crash or silently drop the name
		if i+1 >= len(pairs) {
			attributes = append(attributes, slog.String(name, "(missing)"))
			break
		}
		attributes = append(attributes, slog.Any(name, pairs[i+1]))
	}
	return attributes
}

func formatValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return strconv.Quote(value.String())
	}
	return value.String()
}
