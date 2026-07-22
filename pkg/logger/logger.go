// Package logger holds the logging seam every pkg/* type accepts: the Logger
// interface, plus NewDefaultLogger, the default implementation used when a
// caller doesn't supply one.
package logger

import (
	"context"
	"io"
	"log/slog"
)

// Logger is exactly *slog.Logger's Context method set. Pass your own
// *slog.Logger with whatever slog.Handler you want (zap/zerolog/logr all
// ship one), or anything else that implements these four methods.
type Logger interface {
	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// NewDefaultLogger is the slog default: text lines to w, WARN and up.
// level overrides WARN; extra args are ignored.
func NewDefaultLogger(w io.Writer, level ...slog.Level) *slog.Logger {
	lvl := slog.LevelWarn
	if len(level) > 0 {
		lvl = level[0]
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}
