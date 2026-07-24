package cli

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/migrate"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	topicMigrations "github.com/agentstax/vulkan/pkg/topic/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

// systemEntityID is the fixed entity_id every system-scope schema_log row
// carries -- the control plane is a singleton (see pkg/migrate/datastore).
const systemEntityID = 0

// availableSystemVersion / availableTopicVersion are the version ceilings this
// binary knows: the v1 baseline plus every step compiled into the registry. The
// registry is the source of truth, not the DB -- an older CLI against a newer DB
// reports its own lower ceiling, which is information, not an error.
func availableSystemVersion() int64 { return int64(len(systemMigrations.Registry)) + 1 }
func availableTopicVersion() int64  { return int64(len(topicMigrations.Registry)) + 1 }

func newMigrateCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Initialize and version the control-plane and topic schemas",
	}

	cmd.AddCommand(newMigrateInitCmd(g))
	cmd.AddCommand(newMigrateVersionsCmd())
	cmd.AddCommand(newMigrateStatusCmd(g))
	cmd.AddCommand(newMigrateSystemCmd(g))
	cmd.AddCommand(newMigrateTopicsCmd(g))
	cmd.AddCommand(newMigrateTopicCmd(g))

	return cmd
}

// scope is which schema a migrate command targets.
type scope int

const (
	scopeSystem scope = iota // the shared control-plane schema (one entity)
	scopeTopics              // every registered topic
	scopeTopic               // one topic, by name
)

// direction is the guardrail the operator committed to on the command line.
// It's not passed to the runner (which infers up/down from target vs current);
// it's enforced CLI-side so `down` can never silently roll a schema forward.
type direction int

const (
	dirUp direction = iota
	dirDown
)

func (d direction) verb() string {
	if d == dirDown {
		return "down"
	}
	return "up"
}

// ceiling is the highest version this binary can migrate a scope to.
func (s scope) ceiling() int64 {
	if s == scopeSystem {
		return availableSystemVersion()
	}
	return availableTopicVersion()
}

// migrateTarget is one entity a run touches, paired with its current DB version
// so the direction guard and the no-op check can reason about it before any DDL.
type migrateTarget struct {
	label      string // "system" or a topic name, for messages
	entityType string
	id         int64
	current    int64
}

// gatherTargets resolves the entities a scope covers and reads each one's current
// schema version. Registration gaps surface here as teaching errors, before the
// migrate call, so the operator never sees a raw undefined-table or ErrNotRegistered.
func gatherTargets(ctx context.Context, mAdmin *admin.MessageAdmin, pool *pgxpool.Pool, s scope, name string) ([]migrateTarget, error) {
	switch s {
	case scopeSystem:
		current, err := migrate.Version(ctx, pool, migrate.EntitySystem, systemEntityID)
		if err != nil {
			if errors.Is(err, migrate.ErrNotRegistered) {
				return nil, errSystemNotRegistered()
			}
			return nil, translateAdminError(err)
		}
		return []migrateTarget{{label: "system", entityType: migrate.EntitySystem, id: systemEntityID, current: current}}, nil

	case scopeTopic:
		found, err := mAdmin.GetTopic(ctx, name)
		if err != nil {
			return nil, translateAdminError(err)
		}
		if found == nil {
			return nil, failOp("topic %q not found", name)
		}
		current, err := migrate.Version(ctx, pool, migrate.EntityTopic, found.Id)
		if err != nil {
			return nil, translateAdminError(err)
		}
		return []migrateTarget{{label: found.Name, entityType: migrate.EntityTopic, id: found.Id, current: current}}, nil

	default: // scopeTopics
		topics, err := mAdmin.ListTopics(ctx)
		if err != nil {
			return nil, translateAdminError(err)
		}
		targets := make([]migrateTarget, 0, len(topics))
		for _, t := range topics {
			current, err := migrate.Version(ctx, pool, migrate.EntityTopic, t.Id)
			if err != nil {
				return nil, translateAdminError(err)
			}
			targets = append(targets, migrateTarget{label: t.Name, entityType: migrate.EntityTopic, id: t.Id, current: current})
		}
		return targets, nil
	}
}

// guardDirection rejects a target that sits on the wrong side of the operator's
// chosen direction: `up` must never roll a schema back, `down` must always. The
// runner would happily do either from a bare target -- this is what makes the
// explicit up/down split mean something. Returns the count of targets that will
// actually move (target != current) so the caller can no-op cleanly.
func guardDirection(targets []migrateTarget, dir direction, to int64) (moving int, err error) {
	for _, t := range targets {
		switch {
		case dir == dirUp && to < t.current:
			return 0, failUsage("%s is at version %d; --to %d is a downgrade -- use `down` to roll back", t.label, t.current, to)
		case dir == dirDown && to > t.current:
			return 0, failUsage("%s is at version %d; --to %d is not a downgrade -- use `up` to move forward", t.label, t.current, to)
		}
		if to != t.current {
			moving++
		}
	}
	return moving, nil
}

// errSystemNotRegistered is the single teaching error every path raises when the
// control-plane schema is missing -- one wording, one place to change it.
func errSystemNotRegistered() error {
	return failUsage("system schema not registered -- run `vulkan migrate init` first")
}

// migrateError maps a failed admin migrate call to operator-facing output. Most
// preconditions are caught before the call (gatherTargets/guardDirection); this
// covers the residue -- a lost registration race, or a step that errored midway.
func migrateError(err error) error {
	if errors.Is(err, migrate.ErrNotRegistered) {
		return errSystemNotRegistered()
	}
	return translateAdminError(err)
}
