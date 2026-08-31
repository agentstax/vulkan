package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
)

type StepType string

const (
	StepUp   StepType = "UP"
	StepDown StepType = "DOWN"
)

type Step struct {
	Version              int64
	MinCompatibleVersion int64
	Validate             func(context.Context, datastore.Querier, int64) error
	Apply                func(context.Context, datastore.Querier, int64) error
	NoTxn                bool
}

// SchemaStateRow is one scope's version facts: the version the schema is
// at, and the minimum compatible version in force -- the strictest
// declaration among the steps at or below it.
type SchemaStateRow struct {
	Version              int64 `db:"migration_version"`
	MinCompatibleVersion int64 `db:"min_compatible_version"`
}
