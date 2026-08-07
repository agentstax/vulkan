package base

// consumer worker rows carry no tuning -- each runner paces from its own
// row config
type baseMetadata struct{}

func (m *baseMetadata) Validate() error {
	return nil
}
