package worker

import (
	"github.com/google/uuid"
)

// WorkerInstance is one live copy of a worker.
type WorkerInstance struct {
	Id       int64
	WorkerId int64
	Token    uuid.UUID
	Attempts int
}
