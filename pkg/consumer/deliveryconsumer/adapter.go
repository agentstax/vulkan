package deliveryconsumer

import (
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

func toDeliveryConsumerMetadata(cfg *DeliveryConsumerConfig) *deliveryConsumerMetadata {
	return &deliveryConsumerMetadata{
		ClaimPollRate:       workercontroller.NewMetadataValue(cfg.ClaimPollRate),
		Message:             workercontroller.NewMetadataValue(*cfg.Message),
		ConcurrencyOverride: workercontroller.NewMetadataValue(cfg.ConcurrencyOverride),
	}
}
