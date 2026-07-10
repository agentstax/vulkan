package topic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// ErrTopicConfigMismatch means Register was called with a Config that differs from the topic's existing row
var ErrTopicConfigMismatch = errors.New("topic config does not match existing topic")

// Id addresses this topic's own message_log_<id>.
type Topic struct {
	Id                     int64
	Name                   string
	PartitionSize          int64
	RetentionTTL           time.Duration
	AllowDropPastCommitted bool
}

func Exists(ctx context.Context, ds *datastore.PostgresDatastore, name string) (bool, error) {
	if err := validateName(name); err != nil {
		return false, err
	}

	td := newTopicDatastore(ds)

	found, err := td.GetTopic(ctx, name)
	if err != nil {
		return false, err
	}
	return found != nil, nil
}

// Register is idempotent -- an existing name resolves to its topic instead of erroring.
func Register(ctx context.Context, ds *datastore.PostgresDatastore, cfg Config) (*Topic, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.SetDefaults()

	td := newTopicDatastore(ds)
	return td.UpsertTopic(ctx, cfg)
}

func Destroy(ctx context.Context, ds *datastore.PostgresDatastore, name string) error {
	if err := validateName(name); err != nil {
		return err
	}

	td := newTopicDatastore(ds)

	found, err := td.GetTopic(ctx, name)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("topic %s not found", name)
	}

	return td.DeleteTopic(ctx, found)
}
