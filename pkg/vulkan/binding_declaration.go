package vulkan

import (
	"context"
)

// BindingDeclarationHandle is a consumer group's binding declaration,
// named on its topic and group, holding no row. Get is the comma-ok read.
type BindingDeclarationHandle struct {
	topicName string
	groupName string
	client    *Client
}

// BindingDeclaration names this group's binding declaration. No I/O and
// no failure -- Get resolves both names when called.
func (g *GroupHandle) BindingDeclaration() *BindingDeclarationHandle {
	return &BindingDeclarationHandle{topicName: g.topicName, groupName: g.name, client: g.client}
}

// Get reads the group's effective declaration -- its newest installed
// set. Returns (nil, nil) when the topic or the group is not registered,
// or when the group never declared a set and reads the whole topic.
func (b *BindingDeclarationHandle) Get(ctx context.Context) (*BindingDeclaration, error) {
	return b.client.admin.GetBindingDeclaration(ctx, b.topicName, b.groupName)
}
