package controller

import (
	"encoding/json"
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
)

func ToJobPayload(data json.RawMessage) (*alert.JobPayload, error) {
	var decoded alert.JobPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("job payload: %w", err)
	}
	if err := decoded.Validate(); err != nil {
		return nil, fmt.Errorf("job payload: %w", err)
	}
	return &decoded, nil
}
