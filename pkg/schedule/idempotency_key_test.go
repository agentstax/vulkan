package schedule

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIdempotencyKey(t *testing.T) {
	scheduledTime := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	k := IdempotencyKey(scheduledTime, 42)
	if k != IdempotencyKey(scheduledTime, 42) {
		t.Error("same (scheduledTime, id) must produce the same key")
	}
	if k == IdempotencyKey(scheduledTime, 43) {
		t.Error("different ids in the same ms must produce different keys")
	}
	if k == IdempotencyKey(scheduledTime.Add(time.Minute), 42) {
		t.Error("different scheduled times of the same found must produce different keys")
	}
	if v := k.Version(); v != 7 {
		t.Errorf("version = %d, want 7", v)
	}
	if k.Variant() != uuid.RFC4122 {
		t.Errorf("variant = %v, want RFC 4122", k.Variant())
	}

	// the id is stored verbatim -- decode it back out of the payload bits
	for _, id := range []int64{1, 42, 1<<52 + 7, 1<<62 + 3} {
		k := IdempotencyKey(scheduledTime, id)
		got := int64(uint64(k[6]&0x0f)<<60 | uint64(k[7])<<52 | uint64(k[9]&0x0f)<<48 |
			uint64(k[10])<<40 | uint64(k[11])<<32 | uint64(k[12])<<24 |
			uint64(k[13])<<16 | uint64(k[14])<<8 | uint64(k[15]))
		if got != id {
			t.Errorf("id %d round-tripped to %d", id, got)
		}
	}
}
