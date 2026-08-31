package controller

import (
	"github.com/agentstax/vulkan/pkg/consumergroup/deliveryconsumer/controller/datastore"
)

func toDelivery(data datastore.ExceptionQueueRow) Delivery {
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

func toExceptionQueueRow(delivery *Delivery) *datastore.ExceptionQueueRow {
	return &datastore.ExceptionQueueRow{
		ConsumerGroupId: delivery.ConsumerGroupId,
		TopicId:         delivery.TopicId,
		MessageId:       delivery.MessageId,
		Payload:         delivery.Payload,
		Status:          delivery.Status,
		Attempts:        delivery.Attempts,
		Options:         delivery.Options,
	}
}
