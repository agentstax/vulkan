package controller

import (
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/system/controller/datastore"
)

func toSystem(data *datastore.SystemData) *system.System {
	return &system.System{
		Id:        data.Id,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
	}
}
