package common

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

type bufferLogger struct {
	inner Logger
}

// BufferLogger returns inner wrapped so operations opened by WithLogBuffer
// ship their held records on the failure line. Wrapping is idempotent, and
// LoggerWith keeps the wrapper outermost, so every config may wrap its
// resolved Logger unconditionally.
func BufferLogger(inner Logger) Logger {
	if _, ok := inner.(*bufferLogger); ok {
		return inner
	}
	return &bufferLogger{inner: inner}
}

func (b *bufferLogger) DebugContext(ctx context.Context, message string, args ...any) {
	if buffer, ok := logBufferFrom(ctx); ok {
		buffer.append(time.Now(), slog.LevelDebug, message, args)
	}
	b.inner.DebugContext(ctx, message, args...)
}

func (b *bufferLogger) InfoContext(ctx context.Context, message string, args ...any) {
	if buffer, ok := logBufferFrom(ctx); ok {
		buffer.append(time.Now(), slog.LevelInfo, message, args)
	}
	b.inner.InfoContext(ctx, message, args...)
}

func (b *bufferLogger) WarnContext(ctx context.Context, message string, args ...any) {
	if buffer, ok := logBufferFrom(ctx); ok {
		buffer.append(time.Now(), slog.LevelWarn, message, args)
	}
	b.inner.WarnContext(ctx, message, args...)
}

func (b *bufferLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	if buffer, ok := logBufferFrom(ctx); ok {
		if preceding, held := buffer.drain(); held {
			// Clip forces append to reallocate, never writing into the
			// caller's backing array
			args = append(slices.Clip(args), "preceding", preceding)
		}
	}
	b.inner.ErrorContext(ctx, message, args...)
}
