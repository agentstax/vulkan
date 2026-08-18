package metrics

import (
	"github.com/agentstax/vulkan/pkg/common"
)

type MetricEventConfig struct {
	Noop   bool
	Logger common.Logger
	Retry  *common.RetryPolicy
}
