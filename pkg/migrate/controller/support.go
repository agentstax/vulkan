package controller

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	topicMigrations "github.com/agentstax/vulkan/pkg/topic/migrations"
)

// AssertSystemSchemaSupported gates startup for a system-owned caller: the
// shared system schema must sit within the range this build understands.
// Too new -> upgrade the binary; too old -> migrate the database.
func (c *Controller) AssertSystemSchemaSupported(ctx context.Context, systemId int64) error {
	if systemId <= 0 {
		return fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	state, err := c.datastore.SystemSchemaState(ctx, systemId)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	buildVersion := systemMigrations.Version()
	return assertVersionInRange(common.OwnerSystem, state.Version, buildVersion, buildVersion)
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

	state, err := c.datastore.TopicSchemaState(ctx, topicId)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	buildVersion := topicMigrations.Version()
	return assertVersionInRange(common.OwnerTopic, state.Version, buildVersion, buildVersion)
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
