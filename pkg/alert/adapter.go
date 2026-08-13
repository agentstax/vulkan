package alert

import (
	"encoding/json"
	"fmt"
)

func ToJobData(data json.RawMessage) (*JobData, error) {
	var decoded JobData
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("job data payload: %w", err)
	}
	if err := decoded.Validate(); err != nil {
		return nil, fmt.Errorf("job data payload: %w", err)
	}
	return &decoded, nil
}
