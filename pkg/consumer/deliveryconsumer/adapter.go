package deliveryconsumer

func toDeliveryConsumerMetadata(cfg *DeliveryConsumerConfig) *deliveryConsumerMetadata {
	return &deliveryConsumerMetadata{
		ClaimPollRate:       cfg.ClaimPollRate,
		Message:             *cfg.Message,
		ConcurrencyOverride: cfg.ConcurrencyOverride,
	}
}
