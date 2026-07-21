package topic

import "errors"

// ErrTopicConfigMismatch means Register was called with a Config that differs from the topic's existing row
var ErrTopicConfigMismatch = errors.New("topic config does not match existing topic")

// ErrTopicNotFound means the named topic has no row.
var ErrTopicNotFound = errors.New("topic not found")

// ErrTopicStale means a held *Topic no longer matches its row -- the topic was
// destroyed and re-created under the same name, or its config changed. The
// handle's cached identity can't be trusted; resolve a fresh one with Register.
var ErrTopicStale = errors.New("topic handle is stale")
