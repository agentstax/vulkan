package common

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"
)

type recordedCall struct {
	level   string
	message string
	args    []any
}

type recordingLogger struct {
	calls []recordedCall
}

func (r *recordingLogger) DebugContext(ctx context.Context, message string, args ...any) {
	r.calls = append(r.calls, recordedCall{level: "debug", message: message, args: args})
}

func (r *recordingLogger) InfoContext(ctx context.Context, message string, args ...any) {
	r.calls = append(r.calls, recordedCall{level: "info", message: message, args: args})
}

func (r *recordingLogger) WarnContext(ctx context.Context, message string, args ...any) {
	r.calls = append(r.calls, recordedCall{level: "warn", message: message, args: args})
}

func (r *recordingLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	r.calls = append(r.calls, recordedCall{level: "error", message: message, args: args})
}

func precedingValue(t *testing.T, args []any) (slog.Value, bool) {
	t.Helper()
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok && name == "preceding" {
			value, ok := args[i+1].(slog.Value)
			if !ok {
				t.Fatalf("preceding is %T, want slog.Value", args[i+1])
			}
			return value, true
		}
	}
	return slog.Value{}, false
}

func TestBufferLoggerHoldsAndForwardsBelowError(t *testing.T) {
	recorded := &recordingLogger{}
	logger := BufferLogger(recorded)
	ctx := WithLogBuffer(context.Background())

	logger.DebugContext(ctx, "first", "message_id", 7)
	logger.WarnContext(ctx, "second")
	logger.ErrorContext(ctx, "boom", "topic_id", 3)

	if len(recorded.calls) != 3 {
		t.Fatalf("got %d forwarded calls, want 3", len(recorded.calls))
	}

	preceding, held := precedingValue(t, recorded.calls[2].args)
	if !held {
		t.Fatal("error call carries no preceding attr")
	}
	groups := preceding.Group()
	if len(groups) != 2 {
		t.Fatalf("preceding holds %d records, want 2", len(groups))
	}
	firstAttrs := groups[0].Value.Group()
	found := map[string]string{}
	for _, attr := range firstAttrs {
		found[attr.Key] = attr.Value.String()
	}
	if found["message"] != "first" || found["level"] != "DEBUG" {
		t.Fatalf("first held record renders %v", found)
	}
	if found["message_id"] != "7" {
		t.Fatalf("first held record misses its own attrs: %v", found)
	}
}

func TestBufferLoggerSecondErrorCarriesNoPreceding(t *testing.T) {
	recorded := &recordingLogger{}
	logger := BufferLogger(recorded)
	ctx := WithLogBuffer(context.Background())

	logger.DebugContext(ctx, "held")
	logger.ErrorContext(ctx, "first error")
	logger.ErrorContext(ctx, "second error")

	if _, held := precedingValue(t, recorded.calls[1].args); !held {
		t.Fatal("first error carries no preceding attr")
	}
	if _, held := precedingValue(t, recorded.calls[2].args); held {
		t.Fatal("second error still carries a preceding attr")
	}
}

func TestBufferLoggerWithoutBufferPassesThrough(t *testing.T) {
	recorded := &recordingLogger{}
	logger := BufferLogger(recorded)

	logger.DebugContext(context.Background(), "unbuffered")
	logger.ErrorContext(context.Background(), "boom")

	if _, held := precedingValue(t, recorded.calls[1].args); held {
		t.Fatal("error outside a boundary carries a preceding attr")
	}
}

func TestLogBufferDropsOldestPastCap(t *testing.T) {
	buffer := &logBuffer{}
	for i := range logBufferMaxRecords + 3 {
		buffer.append(time.Now(), slog.LevelDebug, "tick", []any{"i", i})
	}

	drained, held := buffer.drain()
	if !held {
		t.Fatal("full ring drained empty")
	}
	groups := drained.Group()
	// the ring keeps logBufferMaxRecords records plus the dropped_count attr
	if len(groups) != logBufferMaxRecords+1 {
		t.Fatalf("drained %d attrs, want %d", len(groups), logBufferMaxRecords+1)
	}
	last := groups[len(groups)-1]
	if last.Key != "dropped_count" || last.Value.Int64() != 3 {
		t.Fatalf("dropped_count renders %s=%s", last.Key, last.Value.String())
	}

	// the wrapped ring drains oldest-first: three overwritten, so the
	// first survivor is the fourth append and the last is the final one
	assertRecordAttr(t, groups[0], "i", "3")
	assertRecordAttr(t, groups[len(groups)-2], "i", strconv.Itoa(logBufferMaxRecords+2))
}

func assertRecordAttr(t *testing.T, group slog.Attr, key string, want string) {
	t.Helper()
	for _, attr := range group.Value.Group() {
		if attr.Key == key {
			if attr.Value.String() != want {
				t.Fatalf("record %s holds %s=%s, want %s", group.Key, key, attr.Value.String(), want)
			}
			return
		}
	}
	t.Fatalf("record %s holds no %s attr", group.Key, key)
}

func TestBufferLoggerIdempotentThroughLoggerWith(t *testing.T) {
	recorded := &recordingLogger{}
	logger := BufferLogger(LoggerWith(BufferLogger(recorded), "worker", "janitor"))
	ctx := WithLogBuffer(context.Background())

	logger.DebugContext(ctx, "held once")
	logger.ErrorContext(ctx, "boom")

	preceding, held := precedingValue(t, recorded.calls[1].args)
	if !held {
		t.Fatal("error call carries no preceding attr")
	}
	if groups := preceding.Group(); len(groups) != 1 {
		t.Fatalf("preceding holds %d records, want 1 -- the wrapper double-appended", len(groups))
	}
}
