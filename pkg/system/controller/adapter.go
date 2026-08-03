package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/system/controller/datastore"
)

func toSystem(data *datastore.SystemData) *system.System {
	return &system.System{
		Id:                  data.Id,
		AlertRepeatInterval: time.Duration(data.AlertRepeatIntervalNs),
		CreatedAt:           data.CreatedAt,
		UpdatedAt:           data.UpdatedAt,
	}
}

func toRegisterSystemData(cfg *SystemConfig) *datastore.SystemData {
	return &datastore.SystemData{
		AlertRepeatIntervalNs: int64(cfg.AlertRepeatInterval),
	}
}

func toAlterSystemData(cfg *AlterSystemConfig) *datastore.AlterSystemData {
	return &datastore.AlterSystemData{
		AlertRepeatIntervalNs: durationNs(cfg.AlertRepeatInterval),
	}
}

// durationNs widens *time.Duration to the *int64 the _ns columns store,
// passing nil through so COALESCE sees NULL.
func durationNs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ns := int64(*d)
	return &ns
}
