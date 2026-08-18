package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// Supported schema version ranges -- the versions of each owner kind's schema
// this build understands.
const (
	MinSystemVersion int64 = 1
	MaxSystemVersion int64 = 1
	MinTopicVersion  int64 = 1
	MaxTopicVersion  int64 = 1
)

// AssertSchemaSupported gates startup: the schemas the owner depends on --
// the shared system schema, plus the owner topic's schema for topic- and
// group-owned callers -- must sit within the range this build understands.
// Too new -> upgrade the binary; too old -> migrate the database.
func (c *Controller) AssertSchemaSupported(ctx context.Context, owner *common.Owner) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	systemOwner, err := common.NewSystemOwner(owner.SystemId)
	if err != nil {
		return err
	}
	if err := c.assertOwner(ctx, systemOwner, MinSystemVersion, MaxSystemVersion); err != nil {
		return err
	}

	if owner.Kind() == common.OwnerSystem {
		return nil
	}
	topicOwner, err := common.NewTopicOwner(owner.SystemId, owner.TopicId, "")
	if err != nil {
		return err
	}
	return c.assertOwner(ctx, topicOwner, MinTopicVersion, MaxTopicVersion)
}

func (c *Controller) assertOwner(ctx context.Context, owner *common.Owner, minV, maxV int64) error {
	v, err := c.datastore.Version(ctx, owner)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	switch {
	case v < minV:
		return fmt.Errorf("%s schema is version %d but this build needs at least %d -- migrate the database up first", owner.Kind(), v, minV)
	case v > maxV:
		return fmt.Errorf("%s schema is version %d but this build only understands up to %d -- upgrade the binary", owner.Kind(), v, maxV)
	}
	return nil
}
