package compactionreadcost

import (
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

func toCompactionReadCostMetadata(cfg *DefinitionConfig) *compactionReadCostMetadata {
	return &compactionReadCostMetadata{
		RepeatInterval: workercontroller.NewMetadataValue(cfg.RepeatInterval),
	}
}
