package common

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

const logBufferMaxRecords = 64

type logBufferKey struct{}

// WithLogBuffer opens an operation boundary: records logged below Error
// through a BufferLogger carrying this ctx are held in a bounded ring, and
// the operation's first Error record drains the ring into its "preceding"
// group attr. The ring dies with the ctx.
func WithLogBuffer(ctx context.Context) context.Context {
	return context.WithValue(ctx, logBufferKey{}, &logBuffer{})
}

// logBuffer is one operation's ring of held records: the slice grows
// lazily to logBufferMaxRecords, then wraps -- start indexes the oldest
// record and appends overwrite it in place.
type logBuffer struct {
	mutex        sync.Mutex
	records      []logBufferRecord
	start        int
	droppedCount int
}

type logBufferRecord struct {
	loggedAt time.Time
	level    slog.Level
	message  string
	args     []any
}

func (b *logBuffer) append(loggedAt time.Time, level slog.Level, message string, args []any) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	record := logBufferRecord{loggedAt: loggedAt, level: level, message: message, args: args}
	if len(b.records) < logBufferMaxRecords {
		b.records = append(b.records, record)
		return
	}

	b.records[b.start] = record
	b.start = (b.start + 1) % logBufferMaxRecords
	b.droppedCount++
}

// drain renders each held record as a numbered subgroup and resets the
// ring, so only the operation's first Error carries a preceding attr.
func (b *logBuffer) drain() (slog.Value, bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if len(b.records) == 0 {
		return slog.Value{}, false
	}

	attrs := make([]slog.Attr, 0, len(b.records)+1)
	for i := range b.records {
		record := b.records[(b.start+i)%len(b.records)]
		recordAttrs := []slog.Attr{
			slog.Time("logged_at", record.loggedAt),
			slog.String("level", record.level.String()),
			slog.String("message", record.message),
		}
		recordAttrs = append(recordAttrs, toAttrs(record.args)...)
		attrs = append(attrs, slog.Attr{Key: strconv.Itoa(i), Value: slog.GroupValue(recordAttrs...)})
	}
	if b.droppedCount > 0 {
		attrs = append(attrs, slog.Int("dropped_count", b.droppedCount))
	}

	b.records = nil
	b.start = 0
	b.droppedCount = 0
	return slog.GroupValue(attrs...), true
}

// ***************
// *** HELPERS ***
// ***************

func logBufferFrom(ctx context.Context) (*logBuffer, bool) {
	buffer, ok := ctx.Value(logBufferKey{}).(*logBuffer)
	return buffer, ok
}
