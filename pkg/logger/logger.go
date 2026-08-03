// Package logger holds the logging seam every pkg/* type accepts: the Logger
// interface, plus NewDefaultLogger, the default implementation used when a
// caller doesn't supply one.
package logger

import (
	"context"
	"io"
	"log/slog"
	"slices"
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

// With returns a Logger that puts args onto every line. A *slog.Logger
// keeps its own With -- attrs pre-resolve into the handler; anything else
// is wrapped.
func With(l Logger, args ...any) Logger {
	if sl, ok := l.(*slog.Logger); ok {
		return sl.With(args...)
	}
	return &withLogger{inner: l, args: args}
}

type withLogger struct {
	inner Logger
	args  []any
}

// Concat, not append -- a fresh slice per call, so concurrent callers never
// share w.args' backing array.
func (w *withLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	w.inner.DebugContext(ctx, msg, slices.Concat(w.args, args)...)
}

func (w *withLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	w.inner.InfoContext(ctx, msg, slices.Concat(w.args, args)...)
}

func (w *withLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	w.inner.WarnContext(ctx, msg, slices.Concat(w.args, args)...)
}

func (w *withLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	w.inner.ErrorContext(ctx, msg, slices.Concat(w.args, args)...)
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
