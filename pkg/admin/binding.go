package admin

import (
	"context"

	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
)

// ListBindings returns every consumer group binding. Groups with no rows here
// match every event on their topic.
func (a *MessageAdmin) ListBindings(ctx context.Context) ([]*consumercontroller.Binding, error) {
	return a.consumerController.ListBindings(ctx)
}
