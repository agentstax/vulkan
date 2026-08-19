package metricscollector

import (
	"fmt"
	"time"
)

// metricsCollectorMetadata is the config stored on the metrics collector
// worker row.
type metricsCollectorMetadata struct {
	PollRate time.Duration `json:"poll_rate"`
}

func (m *metricsCollectorMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	return nil
}

// ***************
// *** HELPERS ***
// ***************

// defaultMetricsCollectorMetadata is the config the system's declaration
// starts with -- 30s keeps a measurement fresher than the typical scrape interval
// without the snapshot queries running hot.
func defaultMetricsCollectorMetadata() *metricsCollectorMetadata {
	return &metricsCollectorMetadata{PollRate: 30 * time.Second}
}
