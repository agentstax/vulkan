package step

import "fmt"

func Run[T any](fn func() (T, error)) (T, error) {
	result, err := fn()
	if err != nil {
		fmt.Printf("[ERROR] Function execution failed: %v\n", err)
	}
	return result, err
}
