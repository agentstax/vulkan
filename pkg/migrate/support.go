package migrate

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
)

// Supported schema version ranges -- the versions of each entities' schema this
// build understands.
const (
	MinSystemVersion int64 = 1
	MaxSystemVersion int64 = 1
	MinTopicVersion  int64 = 1
	MaxTopicVersion  int64 = 1
)

// AssertSchemaSupported gates producer/consumer startup: the shared system
// schema and this topic's schema must both sit within the range this build
// understands. Too new -> upgrade the binary; too old -> migrate the database.
func AssertSchemaSupported(ctx context.Context, q datastore.Querier, topicID int64) error {
	if err := assertEntity(ctx, q, mDatastore.EntitySystem, 0, MinSystemVersion, MaxSystemVersion); err != nil {
		return err
	}
	return assertEntity(ctx, q, mDatastore.EntityTopic, topicID, MinTopicVersion, MaxTopicVersion)
}

func assertEntity(ctx context.Context, q datastore.Querier, entityType string, entityID, minV, maxV int64) error {
	v, err := mDatastore.Version(ctx, q, entityType, entityID)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	switch {
	case v < minV:
		return fmt.Errorf("%s schema is version %d but this build needs at least %d -- migrate the database up first", entityType, v, minV)
	case v > maxV:
		return fmt.Errorf("%s schema is version %d but this build only understands up to %d -- upgrade the binary", entityType, v, maxV)
	}
	return nil
}
