package system

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrSystemLive means DestroySystem was refused because a worker instance is
// still live -- a manager or consumer is running somewhere.
var ErrSystemLive = diagnostic.NewError("VK0010", diagnostic.Permanent,
	"a worker instance is still live",
	"stop running managers and consumers, or pass DestroyOptions.Force")

// ErrTopicsRegistered means DestroySystem was refused because non-system
// topics are still registered.
var ErrTopicsRegistered = diagnostic.NewError("VK0011", diagnostic.Permanent,
	"topics are still registered",
	"destroy them first, or pass DestroyOptions.Force to destroy them and their messages")
