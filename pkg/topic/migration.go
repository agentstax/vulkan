package topic

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// TopicMigration is the per-topic parallel of system.SystemMigration: its funcs
// also take the topic id, so the SQL can name the per-topic tables
type TopicMigration struct {
	Version      int64                                                               // version this step moves to (Up) / from (Down)
	ValidateUp   func(ctx context.Context, q datastore.Querier, topicID int64) error // preconditions; nil = none
	Up           func(ctx context.Context, q datastore.Querier, topicID int64) error // idempotent -- a retry may re-run it
	ValidateDown func(ctx context.Context, q datastore.Querier, topicID int64) error
	Down         func(ctx context.Context, q datastore.Querier, topicID int64) error
	NoTxn        bool // e.g. CREATE INDEX CONCURRENTLY -- runs on the pool, not a tx
}

// topicMigrations is the ordered registry of topic-scope steps. EMPTY at v1 --
// createTopicLog IS the topic baseline (version 1).
// **Same authoring rules as system.systemMigrations**
var topicMigrations = []TopicMigration{}

// validateRegistry requires versions to be a contiguous 1..N run in slice order
func validateRegistry(ms []TopicMigration) error {
	for i, m := range ms {
		if want := int64(i + 1); m.Version != want {
			return fmt.Errorf("topic migration registry: position %d has version %d, want %d -- versions must be a contiguous 1..N run in slice order", i, m.Version, want)
		}
	}
	return nil
}
