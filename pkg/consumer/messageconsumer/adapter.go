package messageconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/message"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
)

func toMessageMeta(claimed controller.Message, resolved *common.MessageOptions) message.MessageMeta {
	return message.MessageMeta{
		Id:             claimed.Id,
		RoutingKey:     claimed.RoutingKey,
		CompactionKey:  claimed.CompactionKey,
		CompactionRank: claimed.CompactionRank,
		CreatedAt:      claimed.CreatedAt,
		Options:        resolved,
	}
}
