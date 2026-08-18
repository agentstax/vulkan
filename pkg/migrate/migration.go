package migrate

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// Migration is one schema step, shared by every scope -- a sparse struct, so a
// step fills only the fields it needs. Its funcs take a topicId: the system
// scope ignores it (always 0), the topic scope uses it so the SQL can name
// per-topic tables (message_log_<id> etc).
//
// Authoring rules the compiler can't enforce:
//   - Shipped steps are IMMUTABLE: fix a mistake with a new, higher version,
//     never an edit.
//   - Steps are self-contained SQL, FROZEN IN TIME -- never call library code
//     that may change under them.
//   - NoTxn steps can't roll back, so they carry their own partial-state check.
//   - Down is a deliberate rollback, not crash recovery.
type Migration struct {
	Version      int64                                                               // version this step moves to (Up) / from (Down)
	ValidateUp   func(ctx context.Context, q datastore.Querier, topicId int64) error // preconditions; nil = none
	Up           func(ctx context.Context, q datastore.Querier, topicId int64) error // idempotent -- a retry may re-run it
	ValidateDown func(ctx context.Context, q datastore.Querier, topicId int64) error
	Down         func(ctx context.Context, q datastore.Querier, topicId int64) error
	NoTxn        bool // e.g. CREATE INDEX CONCURRENTLY -- runs on the pool, not a tx
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
