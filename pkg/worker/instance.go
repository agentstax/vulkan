package worker

import (
	"time"

	"github.com/google/uuid"
)

// WorkerInstance is one live copy of a worker.
type WorkerInstance struct {
	Id        int64
	WorkerId  int64
	Token     uuid.UUID
	ExpiresAt time.Time
	Attempts  int
}
