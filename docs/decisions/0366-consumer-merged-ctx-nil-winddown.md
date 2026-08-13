---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0366 — Consumer session loops keep their ctx and run under a merged ctx; wind-down returns nil

**Context.** Consumer `Register(ctx)` mirrors the producer's lifecycle, but `Consume(ctx)`/`Janitor(ctx)` are loops whose ctx params already mean "this session" — two ctxs now govern one running loop.

**Decision.** The loop APIs stay untouched; each public entrypoint runs its loops under a merged ctx (`WithCancelCause` plus `AfterFunc(lifecycleCtx)`): whichever side cancels first stops the session, most-restrictive wins. One rule everywhere: the lifetime enters at Register; every other ctx is call-scoped. A requested stop returns nil from `Consume` — a session ending on wind-down is completed work, and an error would make every clean SIGTERM deploy log a failure — while a gated `Produce` errors, because a refused call is refused work. Stop attribution moved to an info log ("consumer stopped, reason=lifecycle/caller context cancelled"), and restart loops still terminate because the next call hits the gate's `ErrShutdownRequested`.

**Consequences.** Start/stop are symmetric in the log stream. **Rejected:** dropping the loop ctx param — the session scope is real. **Rejected:** same-ref ctx validation — it bans deriving child ctxs and cannot detect ancestry anyway.
