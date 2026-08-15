package partitioncount

func toPartitionCountMetadata(cfg *DefinitionConfig) *partitionCountMetadata {
	return &partitionCountMetadata{
		RepeatInterval: cfg.RepeatInterval,
	}
}
