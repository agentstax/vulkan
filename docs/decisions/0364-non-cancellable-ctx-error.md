---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0364 — A non-cancellable lifecycle ctx is an error, with an explicit DisableGracefulShutdown opt-out

**Context.** Registering with `context.Background()` or `TODO()` (`ctx.Done() == nil`) would make graceful shutdown silently not exist. But a legitimate fire-and-forget persona exists: short-lived sequential scripts where Produce returning means committed and nothing is ever left to drain.

**Decision.** Register rejects a non-cancellable ctx with a multi-line teaching error — first line wraps `ErrLifecycleContextNotCancellable` for `errors.Is`, body shows both fixes as paste-able snippets. Fire-and-forget callers opt out explicitly via `DisableGracefulShutdown`: declared intent over the cargo-cult `context.WithCancel` ritual a bare error would breed. `pkg/context.LifecycleContext()` (SIGINT/SIGTERM via `signal.NotifyContext`) is the guided path the error points to, no-arg on purpose — anyone with a real parent ctx has outgrown the helper and uses stdlib directly.

**Consequences.** Known wart: the package name shadows stdlib `context` (import alias needed) — moot once the public API moves to root. The consumer got its own `DisableGracefulShutdown` with a different justification ("Consume's ctx is my only off-switch" is a coherent style, not a script persona). The producer's copy of the check and flag were deleted 2026-08-08 along with producer lifecycle capture; the consumer keeps its own.
