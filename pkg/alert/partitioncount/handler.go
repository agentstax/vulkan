package partitioncount

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount/datastore"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

type Handler struct {
	name      string
	topics    *topiccontroller.TopicController
	datastore *datastore.PartitionCountDatastore
	publisher *alert.Publisher
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewHandler(ds *coredatastore.PostgresDatastore, publisher *alert.Publisher, cfg *HandlerConfig) (*Handler, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if publisher == nil {
		return nil, errors.New("publisher must not be nil")
	}
	if cfg == nil {
		cfg = &HandlerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	topics, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	alertDatastore, err := datastore.NewPartitionCountDatastore(ds, &datastore.PartitionCountDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Handler{
		name:      AlertPartitionCount,
		topics:    topics,
		datastore: alertDatastore,
		publisher: publisher,
	}, nil
}

func (h *Handler) Handle(ctx context.Context, request *cron.JobRequest) error {
	jobData, err := alert.ToJobData(request.Data)
	if err != nil {
		return err
	}

	ceiling, err := h.datastore.PartitionLockCeiling(ctx)
	if err != nil {
		return err
	}
	if jobData.Threshold == 0 {
		jobData.Threshold = ceiling / warnDivisor
	}

	topics, err := h.topics.ListTopics(ctx)
	if err != nil {
		return err
	}

	// one topic's failure never skips the others
	var errs error
	for _, topic := range topics {
		owner, err := common.NewTopicOwner(topic.SystemId, topic.Id, topic.Name)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		found, err := h.evaluate(ctx, owner, ceiling, jobData.Threshold)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		err = h.publisher.Publish(ctx, h.name, owner, found)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
	}
	return errs
}

// evaluate measures one topic and returns its alert, nil when none applies.
func (h *Handler) evaluate(ctx context.Context, owner *common.Owner, ceiling int64, threshold int64) (*alert.Alert, error) {
	count, err := h.datastore.PartitionCount(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if count < threshold {
		return nil, nil
	}
	return newPartitionCountAlert(owner, count, ceiling, threshold)
}
