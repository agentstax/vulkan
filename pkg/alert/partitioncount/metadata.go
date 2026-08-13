package partitioncount

// partitionCountMetadata is the worker row's own tuning; the row carries no
// knobs yet.
type partitionCountMetadata struct{}

func (m *partitionCountMetadata) Validate() error {
	return nil
}
