package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/producer"
)

// ProducerHandle is a topic's name plus the client, holding no row.
type ProducerHandle struct {
	topicName string
	client    *Client
}

// Producer names a topic to produce to. No I/O and no failure -- Register
// resolves the topic when called.
func (c *Client) Producer(topicName string) *ProducerHandle {
	return &ProducerHandle{topicName: topicName, client: c}
}

// Register resolves the named topic and returns an instance that produces
// Message to it. cfg may be nil or a sparse struct; a Logger or Retry left
// nil takes the client's.
func (p *ProducerHandle) Register[Message Versioned](ctx context.Context, cfg *ProducerConfig) (*ProducerInstance[Message], error) {
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	if cfg.Logger == nil {
		cfg.Logger = p.client.Logger
	}
	if cfg.Retry == nil {
		cfg.Retry = p.client.Config.Retry
	}

	messageProducer, err := producer.NewProducer(p.client.ds, cfg)
	if err != nil {
		return nil, err
	}

	instance, err := messageProducer.Register[Message](ctx, p.topicName)
	if err != nil {
		return nil, err
	}
	return newProducerInstance(instance)
}
