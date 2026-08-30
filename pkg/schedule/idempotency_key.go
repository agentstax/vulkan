package schedule

import (
	"time"
	"uuid"

	"github.com/agentstax/vulkan/pkg/common"
)

// TopicName is __system.schedules -- the target topic of the system-owned
// schedules (the built-in alert checks); user schedules target their own.
const TopicName = common.SystemTopicPrefix + "schedules"

// IdempotencyKey is the deterministic idempotency key for one (schedule, scheduled
// time): the same JobRequest replayed after an ambiguous commit dedupes,
// everything else lands. UUIDv7 layout -- the scheduled time's unix ms in the
// 48 time bits (the idempotency index wants time-ordered keys), the schedule id
// VERBATIM across the payload bits. NO hash: the idempotency table is shared
// per-topic, and a same-ms hash collision would silently swallow another
// schedule's request.
func IdempotencyKey(scheduledAt time.Time, scheduleId int64) uuid.UUID {
	var k uuid.UUID
	ms := uint64(scheduledAt.UnixMilli())
	k[0] = byte(ms >> 40)
	k[1] = byte(ms >> 32)
	k[2] = byte(ms >> 24)
	k[3] = byte(ms >> 16)
	k[4] = byte(ms >> 8)
	k[5] = byte(ms)

	// id bits 63..52 fill rand_a; id bits 51..0 fill rand_b's low 52 bits
	id := uint64(scheduleId)
	k[6] = 0x70 | byte(id>>60) // version 7
	k[7] = byte(id >> 52)
	k[8] = 0x80 // variant
	k[9] = byte(id>>48) & 0x0f
	k[10] = byte(id >> 40)
	k[11] = byte(id >> 32)
	k[12] = byte(id >> 24)
	k[13] = byte(id >> 16)
	k[14] = byte(id >> 8)
	k[15] = byte(id)
	return k
}
