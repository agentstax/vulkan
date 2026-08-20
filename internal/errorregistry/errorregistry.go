package errorregistry

// Package errorregistry links every package that declares coded errors, so
// an importer sees the complete registry through common.Errors(). The
// registry-wide convention tests import it; a new errors.go that declares
// codes gets its package added here (the completeness test fails until it
// is).

import (
	_ "github.com/agentstax/vulkan/pkg/admin"
	_ "github.com/agentstax/vulkan/pkg/common"
	_ "github.com/agentstax/vulkan/pkg/consumer/controller"
	_ "github.com/agentstax/vulkan/pkg/cron"
	_ "github.com/agentstax/vulkan/pkg/migrate"
	_ "github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	_ "github.com/agentstax/vulkan/pkg/system"
	_ "github.com/agentstax/vulkan/pkg/topic"
	_ "github.com/agentstax/vulkan/pkg/worker"
)
