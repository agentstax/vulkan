package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
)

// StoredMessage is a schedule_config row's payload at the row's
// schema_version, produced as-is: the JSON goes to the message row
// unchanged and SchemaVersion answers with the stored version, so the
// producer path needs no Message type at produce time.
type StoredMessage struct {
	Payload json.RawMessage
	Version int
}

func NewStoredMessage(payload json.RawMessage, version int) (*StoredMessage, error) {
	if len(payload) == 0 {
		return nil, errors.New("payload must not be empty")
	}
	if version <= 0 {
		return nil, fmt.Errorf("version must be > 0, got %d", version)
	}
	return &StoredMessage{Payload: payload, Version: version}, nil
}

func (m StoredMessage) SchemaVersion() int {
	return m.Version
}

func (m StoredMessage) MarshalJSON() ([]byte, error) {
	return m.Payload, nil
}
