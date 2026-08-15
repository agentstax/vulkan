package controller

// MetadataValue is one tunable worker-metadata key, stored as two layers:
// Default is written by whoever declares the worker.
// Override is written by an alter verb and survives redeclaration until
// explicitly cleared.
type MetadataValue[T any] struct {
	Default  T  `json:"default"`
	Override *T `json:"override,omitempty"`
}

func NewMetadataValue[T any](defaultValue T) MetadataValue[T] {
	return MetadataValue[T]{Default: defaultValue}
}

// Effective is the value the worker runs with: Override when set, Default
// otherwise.
func (m MetadataValue[T]) Effective() T {
	if m.Override != nil {
		return *m.Override
	}
	return m.Default
}
