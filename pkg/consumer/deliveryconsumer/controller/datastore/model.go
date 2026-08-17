package datastore

import (
	"encoding/json"

	"github.com/agentstax/vulkan/pkg/common"
)

// DeliveryData is one (consumer_group_id, message_id) row of the per-topic
// delivery_<topic_id> table: the mutable per-consumer lifecycle state that
// lives off the immutable message_log. Payload is not stored on the row --
// it's joined back in from message_log at claim time. The lifecycle claim
// path never sets the lease columns (lease_until / lease_token) -- no crash
// recovery there; the exception window is what leases through them.
type DeliveryData struct {
	ConsumerGroupId int64                  `db:"consumer_group_id"`
	TopicId         int64                  `db:"topic_id"`
	MessageId       int64                  `db:"message_id"`
	Payload         json.RawMessage        `db:"payload"`
	Status          string                 `db:"status"`
	Attempts        int                    `db:"attempts"`
	Options         *common.MessageOptions `db:"options"`
}
