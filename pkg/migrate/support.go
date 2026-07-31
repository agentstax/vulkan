package migrate

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
)

// Supported schema version ranges -- the versions of each owner kind's schema
// this build understands.
const (
	MinSystemVersion int64 = 1
	MaxSystemVersion int64 = 1
	MinTopicVersion  int64 = 1
	MaxTopicVersion  int64 = 1
)

// AssertSchemaSupported gates producer/consumer startup: the shared system
// schema and this topic's schema must both sit within the range this build
// understands. Too new -> upgrade the binary; too old -> migrate the database.
func AssertSchemaSupported(ctx context.Context, q datastore.Querier, systemID int64, topicID int64) error {
	systemOwner, err := common.NewSystemOwner(systemID)
	if err != nil {
		return err
	}
	if err := assertOwner(ctx, q, systemOwner, MinSystemVersion, MaxSystemVersion); err != nil {
		return err
	}
	topicOwner, err := common.NewTopicOwner(systemID, topicID, "")
	if err != nil {
		return err
	}
	return assertOwner(ctx, q, topicOwner, MinTopicVersion, MaxTopicVersion)
}

// AssertSystemSchemaSupported is the topic-less half of AssertSchemaSupported,
// for callers that hold no topic row to read the system id from.
func AssertSystemSchemaSupported(ctx context.Context, q datastore.Querier) error {
	owner, err := mDatastore.SystemOwner(ctx, q)
	if err != nil {
		return err
	}
	return assertOwner(ctx, q, owner, MinSystemVersion, MaxSystemVersion)
}

func assertOwner(ctx context.Context, q datastore.Querier, owner common.Owner, minV, maxV int64) error {
	v, err := mDatastore.Version(ctx, q, owner)
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
