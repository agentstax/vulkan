package alert

import "fmt"

type JobPayload struct {
	Threshold int64 `json:"threshold"` // 0 = Evaluate derives the alert's live default
}

func (JobPayload) SchemaVersion() int { return 1 }

func NewJobPayload(threshold int64) (*JobPayload, error) {
	data := &JobPayload{Threshold: threshold}
	if err := data.Validate(); err != nil {
		return nil, err
	}
	return data, nil
}

func (d *JobPayload) Validate() error {
	if d.Threshold < 0 {
		return fmt.Errorf("threshold must be >= 0, got %d", d.Threshold)
	}
	return nil
}
