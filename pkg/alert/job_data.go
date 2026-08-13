package alert

import "fmt"

type JobData struct {
	Threshold int64 `json:"threshold"` // 0 = the handler derives its threshold live, or uses its default
}

func NewJobData(threshold int64) (*JobData, error) {
	data := &JobData{Threshold: threshold}
	if err := data.Validate(); err != nil {
		return nil, err
	}
	return data, nil
}

func (d *JobData) Validate() error {
	if d.Threshold < 0 {
		return fmt.Errorf("threshold must be >= 0, got %d", d.Threshold)
	}
	return nil
}
