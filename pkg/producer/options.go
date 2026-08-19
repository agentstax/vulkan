package producer

import "github.com/agentstax/vulkan/pkg/producer/controller"

// ProduceOptions holds per-message knobs; the type and its docs live with the
// controller that validates it.
type ProduceOptions = controller.ProduceOptions

// CompactionOptions is ProduceOptions.Compaction; the type and its docs live
// with the controller that validates it.
type CompactionOptions = controller.CompactionOptions

// NewCompactionOptions builds the Compaction option for a produce. Pass rank
// 0 to let arrival order pick the key's winner.
func NewCompactionOptions(key string, rank int64) (*CompactionOptions, error) {
	return controller.NewCompactionOptions(key, rank)
}
