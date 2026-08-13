package producer

import (
	"github.com/agentstax/vulkan/pkg/producer/controller"
)

// MessageRow is one stored message; the struct and its docs live with the
// controller.
type MessageRow[Message any] = controller.MessageRow[Message]
