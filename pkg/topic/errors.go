package topic

import "errors"

// ErrTopicConfigMismatch means Register was called with a PartitionSize the topic wasn't created with.
// Every other config field can be changed by registering again.
var ErrTopicConfigMismatch = errors.New("topic partition size does not match existing topic")

// ErrTopicNotFound means the named topic has no row.
var ErrTopicNotFound = errors.New("topic not found")

// ErrTopicNotEmpty means Destroy was called on a topic that still holds
// messages, without an explicit force override.
var ErrTopicNotEmpty = errors.New("topic still holds messages")

// ErrTopicNameTaken means Rename's target name already belongs to another topic.
var ErrTopicNameTaken = errors.New("topic name already taken")
