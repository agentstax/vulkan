package controller

import (
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/consume/controller/datastore"
)

func toGroup(data *datastore.ConsumerGroupConfigRow) *consume.Group {
	return &consume.Group{
		Id:        data.Id,
		TopicId:   data.TopicId,
		Name:      data.Name,
		CreatedAt: data.CreatedAt,
	}
}

func toBinding(data *datastore.BindingConfigLogRow) *consume.Binding {
	return &consume.Binding{
		GroupName:   data.GroupName,
		TopicName:   data.TopicName,
		Status:      consume.BindingOutcome(data.Status),
		Patterns:    data.Patterns,
		DeclaredBy:  data.DeclaredBy,
		DeclaredAt:  data.DeclaredAt,
		AttemptedAt: data.AttemptedAt,
	}
}
