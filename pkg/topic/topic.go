package topic

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/retry"
)

// Id addresses this topic's own message_log_<id>.
type Topic struct {
	Id                     int64
	Name                   string
	PartitionSize          int64
	RetentionTTL           time.Duration
	AllowDropPastCommitted bool
	IdempotencyKeyTTL      time.Duration
	DisableDeliveryLog     bool
	JanitorPollRate        time.Duration
	JanitorSweepBatchSize  int
}

// TODO - consider constructing an admin object which gets passed ds
// then these Exists, Register, Destroy commands only get ctx, name/topic
// and we no longer have to construct NewTopicDatastore in each
// but might be slightly worse ux for devs

func Exists(ctx context.Context, ds *datastore.PostgresDatastore, name string) (bool, error) {
	if err := validateName(name); err != nil {
		return false, err
	}

	td, err := NewTopicDatastore(ds, nil, retry.NewDefaultRetryPolicy())
	if err != nil {
		return false, err
	}

	found, err := td.GetTopic(ctx, name)
	if err != nil {
		return false, err
	}
	return found != nil, nil
}

// Register is idempotent -- an existing name resolves to its topic instead of erroring.
//
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func Register(ctx context.Context, ds *datastore.PostgresDatastore, cfg *Config) (*Topic, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	td, err := NewTopicDatastore(ds, cfg.Logger, cfg.Retry)
	if err != nil {
		return nil, err
	}
	return td.UpsertTopic(ctx, *cfg)
}

func Destroy(ctx context.Context, ds *datastore.PostgresDatastore, name string) error {
	if err := validateName(name); err != nil {
		return err
	}

	td, err := NewTopicDatastore(ds, nil, retry.NewDefaultRetryPolicy())
	if err != nil {
		return err
	}

	found, err := td.GetTopic(ctx, name)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("%w: %s", ErrTopicNotFound, name)
	}

	return td.DeleteTopic(ctx, found)
}
