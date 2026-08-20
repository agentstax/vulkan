package controller

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
)

// AssertSystemSchemaSupported gates startup for a system-owned caller: the
// shared system schema must sit within the range this build understands.
// Too new -> upgrade the binary; too old -> migrate the database.
func (c *Controller) AssertSystemSchemaSupported(ctx context.Context, systemId int64) error {
	if systemId <= 0 {
		return fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	version, err := c.datastore.SystemVersion(ctx, systemId)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	return assertVersionInRange(common.OwnerSystem, version, migrate.MinSystemVersion, migrate.MaxSystemVersion)
}

// AssertTopicSchemaSupported gates startup for a topic- or group-owned
// caller: the shared system schema plus the topic's own schema must both sit
// within the range this build understands.
func (c *Controller) AssertTopicSchemaSupported(ctx context.Context, systemId int64, topicId int64) error {
	if err := c.AssertSystemSchemaSupported(ctx, systemId); err != nil {
		return err
	}
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}

	version, err := c.datastore.TopicVersion(ctx, topicId)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	return assertVersionInRange(common.OwnerTopic, version, migrate.MinTopicVersion, migrate.MaxTopicVersion)
}

// ***************
// *** HELPERS ***
// ***************

func assertVersionInRange(kind common.OwnerKind, version int64, minVersion int64, maxVersion int64) error {
	switch {
	case version < minVersion:
		return migrate.ErrSchemaOlderThanBuild.With("kind", kind, "version", version, "min_version", minVersion)
	case version > maxVersion:
		return migrate.ErrSchemaNewerThanBuild.With("kind", kind, "version", version, "max_version", maxVersion)
	}
	return nil
}
