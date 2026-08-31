package controller

import (
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/system/controller/datastore"
)

func toSystemData(data *datastore.SystemConfigRow) *system.SystemData {
	return &system.SystemData{
		Id:        data.Id,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
	}
}
