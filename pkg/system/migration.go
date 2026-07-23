package system

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// SystemMigration is one system-scope schema step -- a sparse struct, so a step
// fills only the fields it needs.
type SystemMigration struct {
	Version      int64                                                // version this step moves to (Up) / from (Down)
	ValidateUp   func(ctx context.Context, q datastore.Querier) error // preconditions; nil = none
	Up           func(ctx context.Context, q datastore.Querier) error // idempotent -- a retry may re-run it
	ValidateDown func(ctx context.Context, q datastore.Querier) error
	Down         func(ctx context.Context, q datastore.Querier) error
	NoTxn        bool // e.g. CREATE INDEX CONCURRENTLY -- runs on the pool, not a tx
}

// systemMigrations is the ordered registry of system-scope steps. EMPTY at v1 --
// registerSystem IS version 1.
//
// Authoring rules the compiler can't enforce:
//   - Shipped steps are IMMUTABLE: fix a mistake with a new, higher version,
//     never an edit. (The baseline is editable only until the first release.)
//   - Steps are self-contained SQL, FROZEN IN TIME -- never call library code
//     that may change under them.
//   - NoTxn steps can't roll back, so they carry their own partial-state check.
//   - Down is a deliberate rollback, not crash recovery; Validate* observes,
//     never mutates.
var systemMigrations = []SystemMigration{}

// validateRegistry requires versions to be a contiguous 1..N run in slice order
func validateRegistry(ms []SystemMigration) error {
	for i, m := range ms {
		if want := int64(i + 1); m.Version != want {
			return fmt.Errorf("system migration registry: position %d has version %d, want %d -- versions must be a contiguous 1..N run in slice order", i, m.Version, want)
		}
	}
	return nil
}
