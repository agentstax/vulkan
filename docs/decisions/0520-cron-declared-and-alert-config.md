---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0520 — Cron jobs are declared like every other resource; built-in alert config moves into RegisterSystem (amends 0518)

**Context.** [0518] made code the only config writer, but left cron unresolved.
Cron carries the same two-owner conflict as topic -- RegisterCronJob asserts
and raises ErrCronJobConfigMismatch, AlterCronJob mutates the same fields --
and the repo had already worked around it a third way: ensureSystemCronJob
creates the job only when missing, with the comment "RegisterCronJob is not
used directly -- its config-mismatch check would error on a job an operator
has altered after creation". Three mechanisms for one fact. Blocking the
obvious sweep: the built-in alerts' schedule and threshold are consts in
vulkan's own source (partitioncount/job.go `const schedule = "@hourly"`,
`alert.NewJobData(0)`), so `cron alter` is today the ONLY way a user can reach
them, and alertlab asserts exactly that survival.

**Decision.**
- Cron job fields (schedule, timeout, concurrency, data, metadata) are
  declaration. RegisterCronJob becomes latest-write-wins and
  ErrCronJobConfigMismatch is deleted outright -- cron has no structural field
  like partition_size to keep it alive for. Name stays identity: a different
  name is a different job.
- Each built-in alert grows its own JobConfig (Schedule, Threshold) in its
  package, with the current consts as its WithDefaults values. NewJob takes it.
- admin.RegisterSystemConfig composes systemcontroller.SystemConfig with each
  alert's JobConfig, and RegisterSystem takes it. The wrapper lives in
  pkg/admin because import arrows point downward -- pkg/system sits below
  pkg/alert and cannot name it. SystemConfig stays the system domain's own
  (still empty) knobs.
- ensureSystemCronJob and ensureSystemTopic are deleted. Both exist only to
  dodge the mismatch check, so both collapse into ordinary register calls.
- Register overwrites declared fields and never touches action state.
  Suspended is set by a verb, not declared, so a re-register leaves it alone.
  That is the line between config and action, and it holds everywhere.
- A schedule change re-seeds the next scheduled time and drops a run already
  due under the old schedule. That was an explicit operator action and becomes
  a side effect of a deploy, so RegisterCronJob logs it at Info.

**Consequences.** cron_alter.go, AlertCronJob and AlertCronJobConfig join
[0518]'s deletion sweep; `vulkan cron run`, `suspend` and `resume` stay, being
actions. Built-in alert thresholds and schedules become reachable from Go for
the first time -- today they are unreachable except through the CLI verb being
deleted. RegisterSystem's parameter type changes; call sites passing nil are
unaffected. alertlab's suspension assertion stands while its threshold
assertion inverts: it proves an operator alter survives re-register today, and
must prove a declaration applies instead. Rejected: hanging alert config off
MessageAdminConfig, which avoids the signature change but files
declared-into-the-database state beside instance-local concerns (logger,
retry, AllowDestroy); and keeping cron alter as the one mutable resource,
which would leave vulkan with two config models and no principle separating
them.
