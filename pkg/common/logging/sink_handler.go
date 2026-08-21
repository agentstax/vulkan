package logging

import (
	"context"
	"log/slog"
)

// sinkHandler ends the chain: the record becomes the matching call on
// the user's Logger.
type sinkHandler struct {
	logger Logger
}

func newSinkHandler(logger Logger) *sinkHandler {
	return &sinkHandler{
		logger: logger,
	}
}

func (s *sinkHandler) handle(ctx context.Context, record *record) *record {
	switch {
	case record.level >= slog.LevelError:
		s.logger.ErrorContext(ctx, record.message, record.args...)
	case record.level >= slog.LevelWarn:
		s.logger.WarnContext(ctx, record.message, record.args...)
	case record.level >= slog.LevelInfo:
		s.logger.InfoContext(ctx, record.message, record.args...)
	default:
		s.logger.DebugContext(ctx, record.message, record.args...)
	}
	return record
}
