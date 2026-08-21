package logging

import (
	"context"
	"log/slog"
)

// captureHandler appends every record below Error to the operation's
// ring, when one is open on the ctx. Runs first so the ring holds the
// full narration, including records a later stage suppresses.
type captureHandler struct {
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{}
}

func (c *captureHandler) handle(ctx context.Context, record *record) *record {
	if record.level < slog.LevelError {
		if buffer, ok := logBufferFrom(ctx); ok {
			// the ring holds a value copy -- later stages reassign
			// record.args, and held records must keep the pre-enrichment
			// shape
			buffer.append(*record)
		}
	}
	return record
}
