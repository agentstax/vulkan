package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/topic"
)

type MessageProducer struct {
	Topic          *topic.Topic
	datastore      *producerDatastore[Message]
	topicDatastore *topic.TopicDatastore
	batcher        *batcher[Message]
	lifecycleCtx   context.Context
}
