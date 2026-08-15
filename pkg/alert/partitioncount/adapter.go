package partitioncount

import (
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

func toPartitionCountMetadata(cfg *DefinitionConfig) *partitionCountMetadata {
	return &partitionCountMetadata{
		RepeatInterval: workercontroller.NewMetadataValue(cfg.RepeatInterval),
	}
}
