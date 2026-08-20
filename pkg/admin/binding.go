package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/consumergroup"
)

// ListDeclarations returns every group's effective binding declaration and
// any declarers still waiting to change it.
// Groups with empty (no) binding declaration do not show here - even though
// they work, they just match on every message in their topic.
func (a *MessageAdmin) ListDeclarations(ctx context.Context) ([]*consumergroup.Declaration, error) {
	return a.consumerController.ListDeclarations(ctx)
}
