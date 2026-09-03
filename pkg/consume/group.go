package consume

import "time"

// a consumer group is owned by exactly one topic -- names are unique per
// topic, not globally. Children (cursor, lease, binding) reference Id and
// carry no topic_id of their own; the topic_id FK cascade is the group's
// lifecycle -- destroying the topic destroys it.
type GroupData struct {
	Id        int64     `json:"group_id"`
	TopicId   int64     `json:"topic_id"`
	Name      string    `json:"group"`
	CreatedAt time.Time `json:"created_at"`
}
