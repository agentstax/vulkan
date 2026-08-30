module github.com/agentstax/vulkan/tools/compat

go 1.27.0

// Dev-only module, never tagged or published, and deliberately OUT of the
// repo-root go.work: the workspace would resolve vulkan to the working tree
// and silently defeat the pin (run it with GOWORK=off -- `just compat-lab`).
//
// The require below is the whole point of this module: it pins the vulkan
// this lab drives, independent of the working tree. Until two releases
// exist, the replace points at the working tree, making the run a dry-run
// of the harness itself. At a release checkpoint the driver repoints it at
// a worktree of the prior tag:
//   git worktree add .compat/<prior-tag> <prior-tag>
//   cd tools/compat && go mod edit -replace github.com/agentstax/vulkan=../../.compat/<prior-tag>

require github.com/agentstax/vulkan v0.0.0

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)

replace github.com/agentstax/vulkan => ../..
