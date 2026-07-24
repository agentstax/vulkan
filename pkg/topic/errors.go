package topic

import "errors"

// ErrTopicConfigMismatch means Register was called with a Config that differs from the topic's existing row
var ErrTopicConfigMismatch = errors.New("topic config does not match existing topic")

// ErrTopicNotFound means the named topic has no row.
var ErrTopicNotFound = errors.New("topic not found")

// ErrTopicNotEmpty means Destroy was called on a topic that still holds
// messages, without an explicit force override.
var ErrTopicNotEmpty = errors.New("topic still holds messages")

// ErrTopicNameTaken means Rename's target name already belongs to another topic.
var ErrTopicNameTaken = errors.New("topic name already taken")
