package controller

import (
	"github.com/agentstax/vulkan/pkg/consumer/deliveryconsumer/controller/datastore"
)

func toDelivery(data datastore.DeliveryData) Delivery {
	return Delivery{
		ConsumerGroupId: data.ConsumerGroupId,
		TopicId:         data.TopicId,
		MessageId:       data.MessageId,
		Payload:         data.Payload,
		Status:          data.Status,
		Attempts:        data.Attempts,
		Options:         data.Options,
	}
}

func toDeliveryData(delivery *Delivery) *datastore.DeliveryData {
	return &datastore.DeliveryData{
		ConsumerGroupId: delivery.ConsumerGroupId,
		TopicId:         delivery.TopicId,
		MessageId:       delivery.MessageId,
		Payload:         delivery.Payload,
		Status:          delivery.Status,
		Attempts:        delivery.Attempts,
		Options:         delivery.Options,
	}
}
