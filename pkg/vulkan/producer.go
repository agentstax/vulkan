package vulkan

import "context"

// ProducerHandle is a topic's name plus the client, holding no row.
type ProducerHandle[Message Versioned] struct {
	topicName string
	client    *Client
}

// Producer names this topic as a produce target. No I/O and no failure --
// Register resolves the topic when called.
func (t *TopicHandle[Message]) Producer() *ProducerHandle[Message] {
	return &ProducerHandle[Message]{topicName: t.name, client: t.client}
}

// Register resolves the topic and returns an instance that produces its
// Message. cfg may be nil or a sparse struct; a Logger or Retry left nil
// takes the client's.
func (p *ProducerHandle[Message]) Register(ctx context.Context, cfg *ProducerConfig) (*ProducerInstance[Message], error) {
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	if cfg.Logger == nil {
		cfg.Logger = p.client.Logger
	}
	if cfg.Retry == nil {
		cfg.Retry = p.client.Config.Retry
	}

	instance, err := p.client.producer.Register[Message](ctx, p.topicName, cfg)
	if err != nil {
		return nil, err
	}
	return newProducerInstance(instance)
}
