package common

import "github.com/google/uuid"

// TODO - must validate json serializable for producer / consumer

type Work struct {
	Id    string `json:"id"`
	Age   int    `json:"age"`
	Email string `json:"email"`
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
