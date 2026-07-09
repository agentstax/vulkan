package topic

// Config is separate from Topic so Register can grow (retention, etc.) without a signature change.
type Config struct {
	// Name - stable, unique identifier for this topic.
	//
	// Standard pattern is dot-namespaced by domain and entity: <domain>.<entity>[.<event>].
	// Renaming later is safe -- topics are addressed by id internally, not name.
	// Ex: "orders.created", "billing.invoice.paid"
	Name string

	// PartitionSize - rows per partition. Defaults to 1_000_000 if left unset.
	//
	// Lower values give finer-grained retention drops at the cost of more
	// partitions to maintain; higher values are coarser but cheaper to manage.
	// Tune down for low-volume topics, up for high-throughput ones.
	// Ex: 10_000 for a low-volume audit topic, 5_000_000 for high-throughput ingest.
	PartitionSize int64
}

func (c *Config) SetDefaults() {
	if c.PartitionSize == 0 {
		c.PartitionSize = 1_000_000
	}
}

func (c *Config) Validate() error {
	return validateName(c.Name)
}

func (c *Config) ToTopic(id int64) *Topic {
	return &Topic{
		Id:            id,
		Name:          c.Name,
		PartitionSize: c.PartitionSize,
	}
}
