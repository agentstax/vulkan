package controller

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// Message is one message_log row handed to a consumer, payload included.
type Message struct {
	Id             int64
	Payload        json.RawMessage
	CreatedAt      time.Time
	RoutingKey     string // "" if unset
	CompactionKey  string // "" if unset
	CompactionRank int64
	Options        *common.MessageOptions
}
