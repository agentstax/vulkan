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

func toDeclaration(data *datastore.BindingConfigLogRow) *consumergroup.Declaration {
	return &consumergroup.Declaration{
		GroupName:   data.GroupName,
		TopicName:   data.TopicName,
		Status:      consumergroup.DeclarationOutcome(data.Status),
		Patterns:    data.Patterns,
		DeclaredBy:  data.DeclaredBy,
		DeclaredAt:  data.DeclaredAt,
		AttemptedAt: data.AttemptedAt,
	}
}
