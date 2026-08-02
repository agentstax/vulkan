package datastore

import "errors"

// ErrInstanceLost means the instance row expired or was removed mid-work:
// stop -- a replacement may already be running.
var ErrInstanceLost = errors.New("worker instance lost: its row expired or was removed")
