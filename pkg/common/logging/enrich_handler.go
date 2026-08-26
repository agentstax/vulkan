package logging

import (
	"context"
	"slices"
)

// enrichHandler puts the bound args onto every line. Runs after capture,
// so held records skip the identity attributes the emitted line carries.
type enrichHandler struct {
	args []any
}

func newEnrichHandler(args []any) *enrichHandler {
	return &enrichHandler{args: args}
}

func (e *enrichHandler) handle(ctx context.Context, record *record) *record {
	// Concat, not append -- a fresh slice per call, so concurrent
	// callers never share the bound args' backing array
	record.args = slices.Concat(e.args, record.args)
	return record
}
