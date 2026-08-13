---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0354 — The CLI is a nested Go module, resolved locally through a gitignored go.work

**Context.** `cmd/vulkan` builds on cobra + fang, with glyphs via lipgloss v2, while the library module keeps its dependency set minimal. A shared module would put CLI dependencies into every library consumer's module graph even though the CLI code is never compiled into their binaries. Dated approximately; built across July 2026.

**Decision.** `cmd/vulkan` has its own `go.mod` (`.../cmd/vulkan`). A directory with its own `go.mod` is a separate module, excluded from the parent's package graph entirely, so `go get github.com/agentstax/vulkan` pulls zero CLI dependencies into a consumer's `go.sum`. Local development resolves both modules through a `go.work` that is gitignored.

**Consequences.** This is stronger than "the CLI is not compiled in" — unimported packages never ship either way; what the split removes is module-graph and go.sum pollution: no transitive cobra version constraints leaking into a consumer's build, no CLI supply-chain surface. Costs: the CLI versions and releases independently, and local dev needs the workspace file. **Rejected:** committing `go.work` — it would force the workspace on every consumer and CI checkout. **Rejected:** a `replace` directive — it breaks `go install ...@version` for everyone once published.
