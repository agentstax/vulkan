package compactionreadcost

func toCompactionReadCostMetadata(cfg *DefinitionConfig) *compactionReadCostMetadata {
	return &compactionReadCostMetadata{
		RepeatInterval: cfg.RepeatInterval,
	}
}
