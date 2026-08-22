package controller

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/migrate/controller/datastore"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	topicMigrations "github.com/agentstax/vulkan/pkg/topic/migrations"
)

// AssertSystemSchemaSupported gates startup for a system-owned caller against
// the shared system schema.
// Too new -> upgrade the binary; too old -> migrate the database.
func (c *Controller) AssertSystemSchemaSupported(ctx context.Context, systemId int64) error {
	if systemId <= 0 {
		return fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	state, err := c.datastore.SystemSchemaState(ctx, systemId)
	if err != nil {
		return err // ErrNotRegistered, or a real db error
	}
	return assertVersionSupported(common.OwnerSystem, state, systemMigrations.Version())
}

// AssertTopicSchemaSupported gates startup for a topic- or group-owned
// caller against both the shared system schema and the topic's own schema.
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
	return assertVersionSupported(common.OwnerTopic, state, topicMigrations.Version())
}

// ***************
// *** HELPERS ***
// ***************

// assertVersionSupported allows a build iff
// state.MinCompatibleVersion <= buildVersion <= state.Version, where
// buildVersion is what this binary's registry defines
// state.Version is what the database records
// - database behind the build     -> ErrSchemaOlderThanBuild (migrate up)
// - breaking step past the build  -> ErrSchemaNewerThanBuild (upgrade binary)
// - additive steps past the build -> allowed, the rolling-deploy window
func assertVersionSupported(kind common.OwnerKind, state *datastore.SchemaStateData, buildVersion int64) error {
	switch {
	case state.Version < buildVersion:
		return migrate.ErrSchemaOlderThanBuild.With("kind", kind, "version", state.Version, "build_version", buildVersion)
	case state.MinCompatibleVersion > buildVersion:
		return migrate.ErrSchemaNewerThanBuild.With("kind", kind, "version", state.Version, "min_compatible_version", state.MinCompatibleVersion, "build_version", buildVersion)
	}
	return nil
}
