package admin

import "errors"

// ErrDestroyDisabled means DestroyTopic was called without AllowDestroy set
// on the admin's config.
var ErrDestroyDisabled = errors.New("destroy is disabled -- set MessageAdminConfig.AllowDestroy")
