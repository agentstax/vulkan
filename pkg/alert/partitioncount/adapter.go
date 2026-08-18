package partitioncount

func toPartitionCountMetadata(cfg *PartitionCountConfig) *partitionCountMetadata {
	return &partitionCountMetadata{
		RepeatInterval: cfg.RepeatInterval,
	}
}
