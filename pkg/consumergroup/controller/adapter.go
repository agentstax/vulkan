package controller

import (
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/consumergroup/controller/datastore"
)

func toGroup(data *datastore.GroupData) *consumergroup.Group {
	return &consumergroup.Group{
		Id:        data.Id,
		TopicId:   data.TopicId,
		Name:      data.Name,
		CreatedAt: data.CreatedAt,
	}
}

func toDeclaration(data *datastore.BindingLogData) *consumergroup.Declaration {
	return &consumergroup.Declaration{
		GroupName:     data.GroupName,
		TopicName:     data.TopicName,
		SchemaVersion: data.SchemaVersion,
		Status:        consumergroup.DeclarationOutcome(data.Status),
		Patterns:      data.Patterns,
		DeclaredBy:    data.DeclaredBy,
		DeclaredAt:    data.DeclaredAt,
		AttemptedAt:   data.AttemptedAt,
	}
}
