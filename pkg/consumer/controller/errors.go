package controller

import "errors"

// ErrGroupNotFound means the named group has no row on that topic.
var ErrGroupNotFound = errors.New("consumer group not found")

// ErrGroupLive means Destroy was called while a worker instance still runs
// on the group, without a force override.
var ErrGroupLive = errors.New("consumer group still has a live consumer")

// ErrGroupDeliveriesPending means Destroy was called while the group still
// holds delivery rows, without a force override. Deleting them discards:
//   - ready/inflight/deferred rows -> failures promised a retry
//   - dead rows                    -> the dead-letter record
var ErrGroupDeliveriesPending = errors.New("consumer group still has delivery rows")
