package conventions

// Package conventions enforces the machine-checkable rules of the repo-root
// CONVENTIONS.md as tests. It is developer tooling, not production code:
// nothing imports it, and it ships in the dev-only tools module so its
// dependencies never enter the library's graph. Run it via `just verify`.
//
// The import block below links every package that declares coded errors or
// log events, so the walks see the complete registry through diagnostic.Errors()
// and diagnostic.Events(). A new errors.go or logs.go that declares codes
// gets its package added here (the completeness test fails until it is).

import (
	_ "github.com/agentstax/vulkan/pkg/common"
	_ "github.com/agentstax/vulkan/pkg/consumergroup"
	_ "github.com/agentstax/vulkan/pkg/cron"
	_ "github.com/agentstax/vulkan/pkg/migrate"
	_ "github.com/agentstax/vulkan/pkg/producer"
	_ "github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	_ "github.com/agentstax/vulkan/pkg/system"
	_ "github.com/agentstax/vulkan/pkg/topic"
	_ "github.com/agentstax/vulkan/pkg/worker"
)
