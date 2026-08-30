package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/google/uuid"
)

// TopicName is __system.job_requests
const TopicName = common.SystemTopicPrefix + "job_requests"

// JobRequest is one request to run a schedule -- the scheduler produces one
// per due scheduled time (RunSchedule on demand) to __system.job_requests with
// the schedule's name as the routing key; consumers bind schedule names.
type JobRequest struct {
	ScheduleId  int64           `json:"schedule_id"`
	Name        string          `json:"schedule"`
	ScheduledAt time.Time       `json:"scheduled_at"` // the scheduled time this request represents, not when it was produced
	Payload     json.RawMessage `json:"payload"`
	Metadata    json.RawMessage `json:"metadata"`
}

func (JobRequest) SchemaVersion() int { return 1 }

func NewJobRequest(scheduleId int64, name string, scheduledAt time.Time, payload, metadata json.RawMessage) (*JobRequest, error) {
	if scheduleId <= 0 {
		return nil, fmt.Errorf("scheduleId must be > 0, got %d", scheduleId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if scheduledAt.IsZero() {
		return nil, errors.New("scheduledAt is required")
	}

	return &JobRequest{
		ScheduleId:  scheduleId,
		Name:        name,
		ScheduledAt: scheduledAt,
		Payload:     payload,
		Metadata:    metadata,
	}, nil
}

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
