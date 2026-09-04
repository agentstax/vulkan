package vulkan

import "context"

// ConsumerHandle is a consumer group's name, its topic, and the client,
// holding no row.
type ConsumerHandle struct {
	groupName string
	topicName string
	client    *Client
}

// Consumer names a consumer group on a topic. No I/O and no failure --
// Register resolves both names when called.
func (c *Client) Consumer(groupName string, topicName string) *ConsumerHandle {
	return &ConsumerHandle{groupName: groupName, topicName: topicName, client: c}
}

// Register resolves the named topic and registers the consumer group on it,
// returning an instance that consumes Message from it. cfg is the group's
// declaration -- nil or sparse for the defaults, with cfg.Bindings the full
// pattern set (nil = the whole topic). A Logger or Retry left nil takes the
// client's.
func (c *ConsumerHandle) Register[Message Versioned](ctx context.Context, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	if cfg.Logger == nil {
		cfg.Logger = c.client.Logger
	}
	if cfg.Retry == nil {
		cfg.Retry = c.client.Config.Retry
	}

	instance, err := c.client.consumer.Register[Message](ctx, c.groupName, c.topicName, cfg)
	if err != nil {
		return nil, err
	}
	return newConsumerInstance(instance, c.client.manager, !c.client.Config.DisableManager)
}
