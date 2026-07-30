package consumer

import (
	"time"
)

// a consumer group is global by name -- one resource tracking an independent
// cursor per topic. Children reference Id (name is a mutable display/lookup
// attribute); EntityId is its lifecycle root row in entity.
type Group struct {
	Id        int64
	Name      string
	EntityId  int64
	CreatedAt time.Time
}
