package admin

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/consume"
)

// GetBindingDeclaration reads the group's effective binding declaration --
// its newest installed set. Returns (nil, nil) when the topic or the group
// is absent, or when the group never declared a set.
func (a *MessageAdmin) GetBindingDeclaration(ctx context.Context, topicName string, groupName string) (*consume.BindingDeclaration, error) {
	if groupName == "" {
		return nil, errors.New("group name is required")
	}

	found, err := a.GetTopic(ctx, topicName)
	if err != nil || found == nil {
		return nil, err
	}
	group, err := a.consumerController.GetGroup(ctx, found.Id, groupName)
	if err != nil || group == nil {
		return nil, err
	}
	return a.consumerController.GetBindingDeclaration(ctx, found.Id, group.Id)
}

// ListBindingDeclarations returns every group's effective binding declaration and
// any declarers still waiting to change it.
// Groups with empty (no) binding declaration do not show here - even though
// they work, they just match on every message in their topic.
func (a *MessageAdmin) ListBindingDeclarations(ctx context.Context) ([]*consume.BindingDeclaration, error) {
	return a.consumerController.ListBindingDeclarations(ctx)
}
