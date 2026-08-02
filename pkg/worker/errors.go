package worker

import "github.com/agentstax/vulkan/pkg/worker/datastore"

// ErrInstanceLost means the instance row expired or was removed mid-work:
// stop -- a replacement may already be running.
var ErrInstanceLost = datastore.ErrInstanceLost
