package admin

import "errors"

// ErrDestroyDisabled means DestroyTopic was called without AllowDestroy set
// on the admin's config.
var ErrDestroyDisabled = errors.New("destroy is disabled -- set MessageAdminConfig.AllowDestroy")

// ErrReservedTopicName means Register/Rename touched a name under
// SystemTopicPrefix -- reserved for admin's own system topics.
var ErrReservedTopicName = errors.New("topic name uses the reserved __system. prefix")
