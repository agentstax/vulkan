package compactionreadcost

// compactionReadCostMetadata is the worker row's own tuning; the row carries no
// knobs yet.
type compactionReadCostMetadata struct{}

func (m *compactionReadCostMetadata) Validate() error {
	return nil
}
