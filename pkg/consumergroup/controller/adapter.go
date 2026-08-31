package controller

import (
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/consumergroup/controller/datastore"
)

func toGroupData(data *datastore.ConsumerGroupConfigRow) *consumergroup.GroupData {
	return &consumergroup.GroupData{
		Id:        data.Id,
		TopicId:   data.TopicId,
		Name:      data.Name,
		CreatedAt: data.CreatedAt,
	}
}

func toBindingDeclaration(data *datastore.BindingConfigLogRow) *consumergroup.BindingDeclaration {
	return &consumergroup.BindingDeclaration{
		GroupName:   data.GroupName,
		TopicName:   data.TopicName,
		Status:      consumergroup.BindingOutcome(data.Status),
		Patterns:    data.Patterns,
		DeclaredBy:  data.DeclaredBy,
		DeclaredAt:  data.DeclaredAt,
		AttemptedAt: data.AttemptedAt,
	}
}
