module github.com/agentstax/vulkan/reference

go 1.27.0

// Dev-only module: keeps the waterline reference implementation out of the
// root library module's published zip and its `go test ./...` surface.
// Never tagged or published; resolved locally via the repo-root go.work
// (use ./reference).

require github.com/jackc/pgx/v5 v5.10.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)
