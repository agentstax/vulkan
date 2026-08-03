package migrate

import (
	"context"
	"errors"
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

// AssertSchemaSupported gates startup: the schemas the owner depends on --
// the shared system schema, plus the owner topic's schema for topic- and
// group-owned callers -- must sit within the range this build understands.
// Too new -> upgrade the binary; too old -> migrate the database.
func AssertSchemaSupported(ctx context.Context, q datastore.Querier, owner *common.Owner) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	systemOwner, err := common.NewSystemOwner(owner.SystemId)
	if err != nil {
		return err
	}
	if err := assertOwner(ctx, q, systemOwner, MinSystemVersion, MaxSystemVersion); err != nil {
		return err
	}

	if owner.Kind() == common.OwnerSystem {
		return nil
	}
	topicOwner, err := common.NewTopicOwner(owner.SystemId, owner.TopicId, "")
	if err != nil {
		return err
	}
	return assertOwner(ctx, q, topicOwner, MinTopicVersion, MaxTopicVersion)
}

// TODO - remove after pkg/maintain is deleted
// AssertSystemSchemaSupported is the topic-less half of AssertSchemaSupported,
// for callers that hold no topic row to read the system id from.
func AssertSystemSchemaSupported(ctx context.Context, q datastore.Querier) error {
	owner, err := mDatastore.SystemOwner(ctx, q)
	if err != nil {
		return err
	}
	return assertOwner(ctx, q, owner, MinSystemVersion, MaxSystemVersion)
}

func assertOwner(ctx context.Context, q datastore.Querier, owner *common.Owner, minV, maxV int64) error {
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
