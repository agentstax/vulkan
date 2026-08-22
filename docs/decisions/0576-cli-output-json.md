---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# CLI --output json

## Context

Deferred from [0550]: a flag that json-ifies only errors while results
stay tables is half a feature. Conventions surveyed 2026-08-22 across
kubectl, gh, aws, docker, stripe, gcloud, terraform, and clig.dev for
what results, errors, and especially mutations should emit.

## Decision

`--output <text|json>` is a root persistent flag, default text. In
json mode stdout is always exactly one parseable document per command,
success or failure -- never silence, never prose. A result is the
natural document (object for gets, array for lists); keys follow the
[0575] vocabulary; durations render as unit-carrying strings, so any
document containing one goes through a CLI result struct.

Errors render on stderr as `{"error": {...}}` mirroring
diagnostic.Error's LogValue fields -- code, problem, recovery, values,
cause, fix, docs -- exit codes unchanged. A plain failUsage/failOp or
cobra parse error emits the reduced `{"error": {"problem": ...}}`:
omitted parts are unknown, the exit code stays the usage-vs-op
discriminator. failPrinted sites move their failure into the result
document (topic get emits exists:false), exit code preserved.

Mutations follow the surveyed conventions: a destroy emits a small
what-happened record ({topic, version, topic_id, destroyed:true}
shape); cron run emits the handle it produced
({cron_job, message_id}); rename, suspend, and config alters echo the
get-shape, sharing the get command's result struct. migrate init/up
emit a final summary document only, progress stays on stderr. manager
run produces no result document and rejects the flag.

Guards: -q with --output json is a usage error (contradictory on the
gets, redundant on the lists); a destroy in json mode requires --yes,
so the interactive prompt never fires.

## Consequences

- Every RunE splits into compute-the-result then one render branch at
  the end -- the bulk of the mechanical work.
- CLI-owned result structs exist only where output is composed or
  derived; where output is exactly one tagged read-model, it marshals
  directly, no adapter.
- Rejected: streaming machine-readable events for migrate (terraform's
  category) -- revisit post-v1 if ever.
