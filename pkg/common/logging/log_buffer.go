package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
)

const logBufferMaxRecords = 64

type logBufferKey struct{}

// WithLogBuffer opens an operation boundary: records logged below Error
// through a pipeline carrying this ctx are held in a bounded ring, and
// the operation's first Error record drains the ring into its "preceding"
// group attribute. The ring dies with the ctx.
func WithLogBuffer(ctx context.Context) context.Context {
	return context.WithValue(ctx, logBufferKey{}, &logBuffer{})
}

// logBuffer is one operation's ring of held records: the slice grows
// lazily to logBufferMaxRecords, then wraps -- start indexes the oldest
// record and appends overwrite it in place.
type logBuffer struct {
	mutex        sync.Mutex
	records      []record
	start        int
	droppedCount int
}

func (b *logBuffer) append(record record) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if len(b.records) < logBufferMaxRecords {
		b.records = append(b.records, record)
		return
	}

	b.records[b.start] = record
	b.start = (b.start + 1) % logBufferMaxRecords
	b.droppedCount++
}

// drain renders each held record as a numbered subgroup and resets the
// ring, so only the operation's first Error carries a preceding attribute.
func (b *logBuffer) drain() (slog.Value, bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if len(b.records) == 0 {
		return slog.Value{}, false
	}

	attributes := make([]slog.Attr, 0, len(b.records)+1)
	for i := range b.records {
		record := b.records[(b.start+i)%len(b.records)]
		recordAttributes := []slog.Attr{
			slog.Time("logged_at", record.loggedAt),
			slog.String("level", record.level.String()),
			slog.String("message", record.message),
		}
		recordAttributes = append(recordAttributes, toAttributes(record.args)...)
		group := slog.GroupValue(recordAttributes...)
		attributes = append(attributes, slog.Attr{Key: strconv.Itoa(i), Value: group})
	}
	if b.droppedCount > 0 {
		attributes = append(attributes, slog.Int("dropped_count", b.droppedCount))
	}

	b.records = nil
	b.start = 0
	b.droppedCount = 0
	return slog.GroupValue(attributes...), true
}

// ***************
// *** HELPERS ***
// ***************

func logBufferFrom(ctx context.Context) (*logBuffer, bool) {
	buffer, ok := ctx.Value(logBufferKey{}).(*logBuffer)
	return buffer, ok
}

func toAttributes(pairs []any) []slog.Attr {
	attributes := make([]slog.Attr, 0, (len(pairs)+1)/2)
	for i := 0; i < len(pairs); i += 2 {
		name := fmt.Sprint(pairs[i])

		// a name with no value is a call-site bug; render the gap
		// rather than crash or silently drop the name
		if i+1 >= len(pairs) {
			attributes = append(attributes, slog.String(name, "(missing)"))
			break
		}
		attributes = append(attributes, slog.Any(name, pairs[i+1]))
	}
	return attributes
}
