package producer

import (
	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
)

// MessageData is one stored message; the struct and its docs live in
// pkg/common.
type MessageData[Message topic.Versioned] = controller.MessageData[Message]
