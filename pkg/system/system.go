package system

import (
	"time"
)

// System is the singleton system row, read back by GetSystem.
type System struct {
	Id        int64     `json:"system_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
