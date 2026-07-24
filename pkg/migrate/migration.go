package migrate

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
)

// Migration is one schema step, shared by every entity type -- a sparse struct, so a
// step fills only the fields it needs. Its funcs take an entityID: the system
// scope ignores it (always 0), the topic scope uses it as the topic id so the
// SQL can name per-topic tables (message_log_<id> etc).
//
// Authoring rules the compiler can't enforce:
//   - Shipped steps are IMMUTABLE: fix a mistake with a new, higher version,
//     never an edit.
//   - Steps are self-contained SQL, FROZEN IN TIME -- never call library code
//     that may change under them.
//   - NoTxn steps can't roll back, so they carry their own partial-state check.
//   - Down is a deliberate rollback, not crash recovery.
type Migration struct {
	Version      int64                                                                // version this step moves to (Up) / from (Down)
	ValidateUp   func(ctx context.Context, q datastore.Querier, entityID int64) error // preconditions; nil = none
	Up           func(ctx context.Context, q datastore.Querier, entityID int64) error // idempotent -- a retry may re-run it
	ValidateDown func(ctx context.Context, q datastore.Querier, entityID int64) error
	Down         func(ctx context.Context, q datastore.Querier, entityID int64) error
	NoTxn        bool // e.g. CREATE INDEX CONCURRENTLY -- runs on the pool, not a tx
}

func (m *Migration) ToStep(stepType mDatastore.StepType, targetVersion int64) (*mDatastore.Step, error) {
	switch stepType {
	case mDatastore.StepUp:
		if m.Up == nil {
			return nil, fmt.Errorf("version %d has no Up defined", m.Version)
		}
		return mDatastore.NewStep(targetVersion, m.ValidateUp, m.Up, m.NoTxn)
	case mDatastore.StepDown:
		if m.Down == nil {
			return nil, fmt.Errorf("version %d has no Down defined -- migration is irreversible", m.Version)
		}
		return mDatastore.NewStep(targetVersion, m.ValidateDown, m.Down, m.NoTxn)
	default:
		return nil, fmt.Errorf("invalid stepType %s defined", stepType)
	}
}

// Validate requires versions to be contiguous in slice order starting at 2 (v1
// is every scope's baseline) -- position i holds version i+2. That one rule
// rejects gaps, duplicates, and misordering. Empty is valid.
func Validate(registry []Migration) error {
	for i, m := range registry {
		if want := int64(i + 2); m.Version != want {
			return fmt.Errorf("migration registry: position %d has version %d, want %d -- versions must be contiguous starting at 2 (v1 is the baseline)", i, m.Version, want)
		}
	}
	return nil
}
