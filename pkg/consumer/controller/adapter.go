package controller

import (
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

func toBinding(data *datastore.BindingData) *Binding {
	return &Binding{
		GroupName:     data.GroupName,
		TopicName:     data.TopicName,
		SchemaVersion: data.SchemaVersion,
		Pattern:       data.Pattern,
	}
}
