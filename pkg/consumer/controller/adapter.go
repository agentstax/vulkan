package controller

import (
	"github.com/agentstax/vulkan/pkg/consumer/binding"
	"github.com/agentstax/vulkan/pkg/consumer/controller/datastore"
)

func toGroup(data *datastore.GroupData) *Group {
	return &Group{
		Id:        data.Id,
		TopicId:   data.TopicId,
		Name:      data.Name,
		CreatedAt: data.CreatedAt,
	}
}

func toDeclaration(data *datastore.BindingDeclarationData) *binding.Declaration {
	return &binding.Declaration{
		GroupName:     data.GroupName,
		TopicName:     data.TopicName,
		SchemaVersion: data.SchemaVersion,
		Status:        binding.DeclarationOutcome(data.Status),
		Patterns:      data.Patterns,
		DeclaredBy:    data.DeclaredBy,
		DeclaredAt:    data.DeclaredAt,
		AttemptAt:     data.AttemptAt,
	}
}
