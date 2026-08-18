package compactionreadcost

func toCompactionReadCostMetadata(cfg *CompactionReadCostConfig) *compactionReadCostMetadata {
	return &compactionReadCostMetadata{
		RepeatInterval: cfg.RepeatInterval,
	}
}
