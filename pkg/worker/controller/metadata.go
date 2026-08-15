package controller

import "encoding/json"

// ParseMetadata round-trips a worker row's decoded JSONB into the caller's
// own shape. Callers validate the parsed struct themselves.
func ParseMetadata[Metadata any](metadata any) (*Metadata, error) {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	var parsed Metadata
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}
