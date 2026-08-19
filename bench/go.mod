module github.com/agentstax/vulkan/bench

go 1.26.4

// Dev-only module: keeps benchmark harnesses out of the root library
// module's published zip. Never tagged or published; resolved locally via
// the repo-root go.work (use ./bench).

require github.com/jackc/pgx/v5 v5.10.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)
