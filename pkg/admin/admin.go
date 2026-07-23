package admin

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

type MessageAdmin struct {
	systemDatastore *systemDatastore
	topicDatastore  *topic.TopicDatastore
	allowDestroy    bool
}

func NewMessageAdmin(ds *datastore.PostgresDatastore, cfg *MessageAdminConfig) (*MessageAdmin, error) {
	if cfg == nil {
		cfg = &MessageAdminConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	systemDatastore, err := newSystemDatastore(ds, cfg.Logger, cfg.Retry)
	if err != nil {
		return nil, err
	}

	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Logger, cfg.Retry)
	if err != nil {
		return nil, err
	}

	return &MessageAdmin{
		systemDatastore: systemDatastore,
		topicDatastore:  topicDatastore,
		allowDestroy:    cfg.AllowDestroy,
	}, nil
}
