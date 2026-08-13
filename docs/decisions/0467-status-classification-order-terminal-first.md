---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0467 — Status classification checks terminal outcomes before the not-head "superseded" case

**Context.** In derived job-request status, "superseded" is not recorded anywhere — it is computed as "this group never ran the request and it is no longer the compaction head". But every completed request eventually stops being the head too, so the order of the classification arms decides what history the view reports.

**Decision.** Classification checks a group's terminal `delivery_log` outcomes first — succeeded, then failed — and only then classifies a not-head request as superseded; remaining requests are deferred or pending. A group that ran a since-replaced request therefore keeps its success or failure verdict permanently.

**Consequences.** Terminal outcomes are permanent; "superseded" only speaks for requests a group never ran. Because delivery is per group, the same request can correctly count as ran for a fast group and superseded for a group that never claimed it — `GroupStatus.Superseded` is per group by design.
