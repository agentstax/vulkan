package producer

import (
	"github.com/agentstax/vulkan/pkg/producer/controller"
)

// MessageRow is one stored message; the struct and its docs live in
// pkg/common.
type MessageRow[Message any] = controller.MessageRow[Message]
