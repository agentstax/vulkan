module github.com/agentstax/vulkan/tools

go 1.27.0

// Dev-only module: developer tooling, never imported by production code and
// never tagged or published. Its dependencies stay out of the root library
// module's graph. The parent module github.com/agentstax/vulkan is resolved
// locally via the repo-root go.work (use ./tools) and deliberately has NO
// require line here: it's unpublished, so any placeholder version poisons
// the whole workspace graph.

require (
	github.com/jackc/pgx/v5 v5.10.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)
