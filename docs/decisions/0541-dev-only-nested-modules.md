---
status: accepted
date: 2026-08-19
phase: 14b
---

# examples, bench, and reference become dev-only nested modules

## Context

The roadmap carried "go.mod cleanup after factoring examples into a
separate module," premised on examples weighing down the root go.mod.
Measured, the premise was empty: examples/bench/reference import only pgx
and uuid, and pkg/ itself needs all three direct requires (pgx, uuid,
x/sync) -- `go mod tidy` on the root after the split is a no-op. The real
costs of keeping them in the root module were elsewhere: ~40 lab mains
plus benchmark and reference scratch shipped in the published module zip,
and reference/waterline's tests ran in the root `go test ./...`.

## Decision

Carve each dev-only tree into its own nested module, on the existing
cmd/vulkan / otelvulkan pattern:

- examples/go.mod, bench/go.mod, reference/go.mod; the repo-root go.work
  gains `use` lines for all three.
- No require line for the parent module (unpublished; a placeholder
  version poisons the workspace graph -- same rule as cmd/vulkan).
- Unlike cmd/vulkan and otelvulkan, these three are NEVER tagged or
  published, so they never take a pinned parent require at release. The
  release story stays three modules: root, cmd/vulkan, otelvulkan.

## Consequences

- A directory with its own go.mod is excluded from the parent module's
  zip: `go get github.com/agentstax/vulkan` downloads the library only.
- Root `go test ./...` and `go list ./...` no longer include
  examples/bench/reference packages (65 tests, 70 packages after).
- justfile lab recipes are untouched: `go run examples/phase_1/...` from
  the repo root resolves through the workspace.
- Building examples requires the workspace; there is no module-only build
  of the labs against a published parent version, by design.
- The roadmap's go.mod-cleanup follow-up dissolves: the root go.mod was
  already exactly the library's needs.
