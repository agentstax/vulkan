package alert

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
)

// Evaluator is one alert's condition, measured per owner topic.
//   - nil alert -> the condition doesn't hold
//   - threshold 0 -> the alert derives its live default
type Evaluator interface {
	Evaluate(ctx context.Context, owner *common.Owner, threshold int64) (*Alert, error)
}
