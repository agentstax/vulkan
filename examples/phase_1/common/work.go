package common

import "github.com/google/uuid"

// TODO - must validate json serializable for producer / consumer

type Work struct {
	Id    string `json:"id"`
	Age   int    `json:"age"`
	Email string `json:"email"`
	// SleepMs lets a payload carry its own processing time, so a stream can mix
	// fast and slow messages (the Phase 3 variance proof). 0 = no artificial sleep.
	SleepMs int `json:"sleep_ms,omitempty"`
}

func NewWork(age int, email string) (*Work, error) {
	uuid, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	return &Work{
		Id:    uuid.String(),
		Age:   age,
		Email: email,
	}, nil
}
