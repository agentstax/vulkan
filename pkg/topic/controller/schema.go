package controller

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

func (c *TopicController) AssertSchemaSupported(ctx context.Context, systemId int64, topicId int64) error {
	if systemId <= 0 {
		return fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	owner, err := common.NewTopicOwner(systemId, topicId, "")
	if err != nil {
		return err
	}
	return c.migrateController.AssertSchemaSupported(ctx, owner)
}
