package metrics

import (
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type MetricEventConfig struct {
	Noop   bool
	Logger logger.Logger
	Retry  *retry.Policy
}
