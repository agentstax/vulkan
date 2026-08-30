package worker

import (
	"uuid"
)

// WorkerInstance is one live copy of a worker.
type WorkerInstance struct {
	Id       int64     `json:"id"`
	WorkerId int64     `json:"worker_id"`
	Token    uuid.UUID `json:"token"`
	Attempts int       `json:"attempts"`
}
