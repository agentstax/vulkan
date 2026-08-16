---
status: accepted
date: 2026-08-16
phase: "14"
---

# 0524 — Alert pipeline instrumentation: state gauges pulled, run outcomes pushed

**Context.** [0522]'s follow-on. Alert publishes are durable on
__system.alerts, but a check run's outcome is discarded when it ends: one
failed topic fails the whole cron run, so a run where 8 of 9 topics fired
fine is indistinguishable from a total failure, and nothing records how
many topics were evaluated or how many Record calls actually published.

**Decision.** Two facts, two mechanisms:

- Alert STATE (what is active right now) is derived by the metrics
  collector from the alert compaction heads -- rows that already exist --
  as vulkan.alert.state.* gauges, like every other snapshot it takes.
  No handler changes.
- Check-run OUTCOME (evaluated / failed / published / resolved counts) is
  produced by the alert handler itself at the end of each run, as a
  vulkan.alert.check.* summary with attributes {alert}, through the same
  core producer path as everything else. Head = latest run, retained log =
  one row per run; no cumulative counter state.
- The summary is produced even when the run's joined error is non-nil --
  visibility into partial failure is the point.
- AlertController.Record grows a named-outcome return (published /
  resolved / suppressed classification already exists in classify) so the
  handler can count without a second head read.

**Consequences.** Alerting on alerts becomes possible (topics_failed > 0
scrapes from /metrics). Rejected: cumulative counters (needs durable
counter state; the retained log already is the history); one measurement
per failed topic (series explosion for a fact the joined error and logs
carry); instrumenting only the handler for both facts (state is already
durable -- a second write path for it would duplicate the mechanism).
