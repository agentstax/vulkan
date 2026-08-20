package logging

// The Logger seam every config carries, its stderr default, attr
// enrichment, and the per-operation debug buffer.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
)

// Logger is exactly *slog.Logger's Context method set. Pass your own
// *slog.Logger with whatever slog.Handler you want (zap/zerolog/logr all
// ship one), or anything else that implements these four methods.
type Logger interface {
	DebugContext(ctx context.Context, message string, args ...any)
	InfoContext(ctx context.Context, message string, args ...any)
	WarnContext(ctx context.Context, message string, args ...any)
	ErrorContext(ctx context.Context, message string, args ...any)
}

// LoggerWith returns a Logger that puts args onto every line. A *slog.Logger
// keeps its own With -- attrs pre-resolve into the handler; anything else
// is wrapped.
func LoggerWith(l Logger, args ...any) Logger {
	if sl, ok := l.(*slog.Logger); ok {
		return sl.With(args...)
	}

	// a bufferLogger stays outermost through enrichment so BufferLogger's
	// idempotence guard sees it and never wraps twice
	if b, ok := l.(*bufferLogger); ok {
		return &bufferLogger{inner: LoggerWith(b.inner, args...)}
	}
	return &withLogger{inner: l, args: args}
}

type withLogger struct {
	inner Logger
	args  []any
}

// Concat, not append -- a fresh slice per call, so concurrent callers never
// share w.args' backing array.
func (w *withLogger) DebugContext(ctx context.Context, message string, args ...any) {
	w.inner.DebugContext(ctx, message, slices.Concat(w.args, args)...)
}

func (w *withLogger) InfoContext(ctx context.Context, message string, args ...any) {
	w.inner.InfoContext(ctx, message, slices.Concat(w.args, args)...)
}

func (w *withLogger) WarnContext(ctx context.Context, message string, args ...any) {
	w.inner.WarnContext(ctx, message, slices.Concat(w.args, args)...)
}

func (w *withLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	w.inner.ErrorContext(ctx, message, slices.Concat(w.args, args)...)
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

// Attrs converts alternating name/value pairs into slog attrs.
func Attrs(pairs []any) []slog.Attr {
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
