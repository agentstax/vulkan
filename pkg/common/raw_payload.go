package common

// RawPayload is a message payload kept as the JSON bytes the row stores,
// for readers with no message type in scope: the CLI, an admin-only
// script. Its version is 0, which no declared message type may use, so
// the produce and consume Register verbs refuse it.
type RawPayload []byte

func (RawPayload) SchemaVersion() int {
	return 0
}

func (p RawPayload) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}
	return p, nil
}

func (p *RawPayload) UnmarshalJSON(data []byte) error {
	*p = append((*p)[:0], data...)
	return nil
}
