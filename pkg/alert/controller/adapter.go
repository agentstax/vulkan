package controller

import (
	"encoding/json"
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
)

func ToJobData(data json.RawMessage) (*alert.JobData, error) {
	var decoded alert.JobData
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("job data payload: %w", err)
	}
	if err := decoded.Validate(); err != nil {
		return nil, fmt.Errorf("job data payload: %w", err)
	}
	return &decoded, nil
}
