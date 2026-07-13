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

// NewDefaultLogger is the no-opinions default: human-readable text lines to w, warn level
// and up -- quiet by default without being silent, so a caller who never
// thinks about logging still hears about real problems.
func NewDefaultLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
