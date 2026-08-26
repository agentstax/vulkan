package logging

import (
	"context"
	"log/slog"
	"slices"
)

// drainHandler drains the operation's held records into its first
// Error's "preceding" group attribute. Runs after suppression, so a dropped
// repeat Error leaves the ring for the next emitted one.
type drainHandler struct {
}

func newDrainHandler() *drainHandler {
	return &drainHandler{}
}

func (d *drainHandler) handle(ctx context.Context, record *record) *record {
	if record.level >= slog.LevelError {
		if buffer, ok := logBufferFrom(ctx); ok {
			if preceding, held := buffer.drain(); held {
				// Clip forces append to reallocate, never writing into
				// the caller's backing array
				record.args = append(slices.Clip(record.args), "preceding", preceding)
			}
		}
	}
	return record
}
