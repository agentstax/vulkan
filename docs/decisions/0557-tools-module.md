---
status: accepted
date: 2026-08-20
phase: pre-v1
---

# 0557 -- Developer tooling is a dev-only tools/ module

## Context

internal/ mixed live code (internal/topic) with enforcement code
(internal/errorregistry): nothing structural told a contributor which was
which, and the walks could only use the root library's minimal
dependency set. Research (CockroachDB's lint test package, Tailscale's
depaware, Kubernetes' hack/verify scripts behind make verify) converges
on one pattern: repo-policy checks live in one clearly named home,
outside the product, run by a single blessed command. The repo already
had the isolation mechanism -- go.work resolves dev-only nested modules
(bench, examples, reference) that stay out of the published zip and the
root test surface.

## Decision

New dev-only module `tools/` (github.com/agentstax/vulkan/tools; sixth
go.work entry; never tagged or published; no parent require line, same
as the other dev-only modules). internal/errorregistry moves into it as
`tools/conventions` -- one package, named for the document it enforces,
holding the registry blank-import shim and every machine-checkable
CONVENTIONS.md walk. Future rule families (SELECT * ban, seam-law import
arrows, docs-page drift) land as sibling test files there; convenience
dependencies enter tools/go.mod only, never the root graph.

Leaving the root test surface is deliberate: enforcement moves to the
one blessed command, a new `just verify` recipe (build + vet +
`go test -race` on the root, builds of cmd/vulkan, otelvulkan, examples,
and bench, then the tools walks). CI later runs exactly `just verify`.

Rejected: `hack/` as the directory name (k8s-lineage jargon that
describes nothing -- same class as door/sentinel); internal/conventions
(package naming is a weaker signal than a module boundary, and the walks
would stay confined to the root's dependency set); go/analysis analyzers
for now (a rule graduates only when it needs type information or IDE
surfacing).

## Consequences

internal/ holds only live code. `go test ./...` at the root no longer
runs the walks -- `just verify` is load-bearing and becomes the
pre-commit habit. examples/bench compile breaks (today's lab-fix class)
are caught by verify, not first discovered by the fresh-DB suite.
