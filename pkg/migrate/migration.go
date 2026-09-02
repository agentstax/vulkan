package migrate

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// Migration is one schema step, shared by every scope -- a sparse struct, so a
// step fills only the fields it needs. Its funcs take the two coordinates a
// statement needs to name a table: the schema this installation lives in, and
// a topicId that the system scope ignores (always 0) and the topic scope uses
// for per-topic tables. Both are spelled in the SQL --
// fmt.Sprintf("... %s.message_log_%d ...", schema, topicId) -- because the
// pool's search_path does not name the schema [0631].
//
// Authoring rules the compiler can't enforce:
//   - Shipped steps are IMMUTABLE: fix a mistake with a new, higher version,
//     never an edit.
//   - Steps are self-contained SQL, FROZEN IN TIME -- never call library code
//     that may change under them.
//   - Txn steps run under a 2s lock_timeout -- a lock wait past it rolls the
//     step back and retries. NoTxn steps get no cap.
//   - NoTxn steps can't roll back, so they carry their own partial-state check.
//   - Down is a deliberate rollback, not crash recovery.
//   - Empty steps are fine -- sometimes the version number is the whole
//     point. If MinCompatibleVersion needs to be updated or changed, ship an
//     empty step with the new value.
type Migration struct {
	Version int64 // version this step moves to (Up) / from (Down)

	// MinCompatibleVersion is the oldest build schema version whose SQL still
	// runs correctly against the schema this step produces:
	//   0       -> additive, every older binary survives
	//   Version -> breaking, no older binary survives
	MinCompatibleVersion int64

	ValidateUp   func(ctx context.Context, q datastore.Querier, schema string, topicId int64) error // preconditions; nil = none
	Up           func(ctx context.Context, q datastore.Querier, schema string, topicId int64) error // idempotent -- a retry may re-run it
	ValidateDown func(ctx context.Context, q datastore.Querier, schema string, topicId int64) error
	Down         func(ctx context.Context, q datastore.Querier, schema string, topicId int64) error

	NoTxn bool // e.g. CREATE INDEX CONCURRENTLY -- runs on the pool, not a tx
}

// Validate requires versions to be contiguous in slice order starting at 2 (v1
// is every scope's baseline) -- position i holds version i+2. That one rule
// rejects gaps, duplicates, and misordering. Empty is valid. Each step's
// MinCompatibleVersion must sit between 0 and the step's own version.
func Validate(registry []Migration) error {
	for i, m := range registry {
		if want := int64(i + 2); m.Version != want {
			return fmt.Errorf("migration registry: position %d has version %d, want %d -- versions must be contiguous starting at 2 (v1 is the baseline)", i, m.Version, want)
		}
		if m.MinCompatibleVersion < 0 || m.MinCompatibleVersion > m.Version {
			return fmt.Errorf("migration registry: version %d has MinCompatibleVersion %d, want 0 through %d -- 0 is additive, the step's own version locks out every older build", m.Version, m.MinCompatibleVersion, m.Version)
		}
	}
	return nil
}
