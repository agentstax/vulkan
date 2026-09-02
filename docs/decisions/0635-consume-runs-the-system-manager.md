---
status: accepted
date: 2026-09-02
phase: "pre-v1"
---

# 0635 — Consume runs the system manager

**Context.** The deployment most teams have is a consumer binary, yet upkeep required knowing the SystemManager: playground scenario 08 hand-wired `RunManager` beside `Consume` in an errgroup, and a deployment that skipped it silently accumulated until someone ran `vulkan manager run`. The concept is right for dedicated deployments and wrong as required knowledge. River is the precedent — maintenance runs from every client — and Vulkan needs no leader election to copy it: the manager is already N-way safe, worker claims arbitrate who runs what. The principle is also half-adopted already: `Schedule.Schedule(ctx)` is `client.manager.Run(ctx)`.

**Decision.** Every long-running verb runs the system's upkeep: `Consume` joins `Schedule`, composing the client's SystemManager into its own run group. Long-running verbs on one client share one manager — the client counts its active long-running verbs, the first starts the manager under a client-internal context, the last one out cancels it. The in-process permit still refuses a second explicit `RunManager`; the auto-run path joins the running manager silently instead of erroring. `ClientConfig.DisableManager` is the opt-out — deployment-shaped, client-wide, zero value keeps auto-run on per the inverted-bool rule. A fatal manager exit inside a Consume has no caller to receive it and logs Error.

**Consequences.** Scenario 08 collapses to a plain `Consume`; dedicated-manager deployments set `DisableManager` on consumer pods and keep `vulkan manager run`. The consumer's start line reports the resolved `disable_manager` fact, and the manager's own start line already announces itself, so the behavior change is visible in every pasted log. Accepted risks: the topic janitor runs DDL, so a consumer under a restricted database role needs the manager's privileges or its background workers error — the opt-out is the answer for locked-down deployments; and idle reconcile polling now scales with the consumer fleet rather than the manager count — a slower standby poll is available tuning if it matters. Produce-only binaries still have no long-running call to carry upkeep — VK0063 remains their answer, and any one consumer in the deployment now covers their topics. `Schedule.Schedule` stays per [0621].
