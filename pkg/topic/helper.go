package topic

import "errors"

func validateName(name string) error {
	if name == "" {
		return errors.New("topic name is required")
	}
	return nil
}
