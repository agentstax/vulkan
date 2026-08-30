---
status: accepted
date: 2026-08-30
phase: "pre-v1"
---

# 0623 — the standard library's uuid package replaces github.com/google/uuid

**Context.** Go 1.27 ships a std `uuid` package (RFC 9562): `UUID [16]byte`, infallible `New`/`NewV4`/`NewV7`, `Parse`/`MustParse`, `Nil()`/`Max()`, text marshaling in the same canonical form. The main module's dependency rule wants std lib wherever it fits, and pgx v5 encodes and scans any `[16]byte`-underlying type natively (probed end to end against the dev DB). google/uuid was one of only three main-module deps.

**Decision.** Swap every import to std `uuid`. `NewV7` error checks collapse (std is infallible), `uuid.Nil` comparisons become `uuid.Nil()`, `NewString` becomes `New().String()`. The one missing capability, v5 hashing for `resolveIdempotencyKey`, is vendored: `pkg/producer/controller/uuid_hash.go` copies google/uuid v1.6.0's `NewHash`/`NewSHA1` with a provenance header and marked local diffs, tested against the RFC 9562 example vector. `resolveIdempotencyKey` loses its error return. go.mod drops github.com/google/uuid; deps are std + pgx + x/sync.

**Consequences.** Wire and storage shapes are unchanged — same canonical text form, same pgx binary encoding, so stored keys and tokens survive the swap byte-for-byte. The schedule key test checks version/variant bits directly (std has no `Version`/`Variant` accessors). Nested modules shed the dep on their next tidy; tools/go.mod is edited by hand (a tidy there resolves the unpublished parent remotely — the go.mod comment's warning).
