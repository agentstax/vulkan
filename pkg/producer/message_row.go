package producer

import (
	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
)

// MessageRow is one stored message; the struct and its docs live in
// pkg/common.
type MessageRow[Message topic.Versioned] = controller.MessageRow[Message]
