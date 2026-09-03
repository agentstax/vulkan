---
status: accepted
date: 2026-09-02
phase: "pre-v1"
---

# 0642 — The client's own ConsumerInstance runs the manager beside Consume

**Context.** [0638] routed auto-run through
`consumer.ConsumerConfig.RunSystemManager func(ctx) error`, filled by the
client. Built, it was rejected on review: a config holds static values, and a
runnable on it betrays what a config is. The alternative of building the
SystemManager inside `pkg/consumer` behind a static flag is impossible — the
alert packages import `pkg/consumer` for their check consumers and
`systemmanager` assembles the alerts, so `consumer → systemmanager` is an
import cycle. The one package already holding both handles is `pkg/vulkan`.

**Decision.** `vulkan.ConsumerInstance` stops being an alias of
`consumer.ConsumerInstance` — Go forbids methods on another package's type —
and becomes a struct embedding it, holding the client's `SystemManager` and a
`runManager` bool (`!ClientConfig.DisableManager`). Its `Consume` runs the
embedded session and `manager.Run` in an errgroup; embedding promotes every
other verb and field, and the `Consumer[Message]` interface stays satisfied.
`pkg/consumer` reverts to knowing nothing: no func field, config all static
values, no start-line change — the `disable_manager` attribute and its
registry row are dropped, since the consumer no longer knows. Two guards in
the wrapper: a ctx with nil `Done()` passes straight through so the session's
own `ErrLifecycleContextNotCancellable` guard still trips (the errgroup's
derived ctx is always cancellable and would mask it), and the manager runs
under its own cancel released when the session returns — errgroup cancels
only on a non-nil error, so a nil session return would otherwise leave the
manager running and `Wait` blocked forever.

**Consequences.** The composition lives beside the two things it composes and
the func disappears instead of moving. `vulkan.ConsumerInstance` and
`consumer.ConsumerInstance` are now distinct types: method calls and promoted
fields are source-compatible (the examples tree compiles untouched), but
assigning one pointer type to the other no longer compiles. The layered path
is the deliberate opt-out surface: `consumer.NewConsumer → Register` hands
back the plain instance with no manager attached. Verified live under
`-race`: a plain Consume has one live system manager beside it and releases
it on session end; a DisableManager client's Consume runs zero; both end
clean. Supersedes [0638]'s clause "`Consume` reaches it through
`consumer.ConsumerConfig.RunSystemManager func(ctx) error`, filled by the
client unless `ClientConfig.DisableManager` is set (nil is the fact the start
line reports as `disable_manager`)"; the rest of 0638 stands. **Rejected:**
the func field on the config — settled: a config holds static values, never a
runnable; building the manager in `pkg/consumer` (the import cycle); a
runnable param on `Consume` or `NewConsumer`, which moves the nil-encodes-
optionality sin into a signature every direct caller sees.
