---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0468 — Registry verbs: idempotent `RegisterCronJob` that errors on config mismatch, `Alter` re-seeds `next_scheduled_time`, destroy gated by `AllowDestroy`

**Context.** Cron jobs are registered from application code at startup, so registration must be safe to repeat; changing a schedule must not leave a stale due time behind; and deleting a job is destructive enough to need an explicit gate.

**Decision.** `RegisterCronJob` is idempotent — re-registering the same spec is a no-op, while re-registering with a different config errors instead of silently overwriting. Every `cron_job` row requires exactly one owner. `Alter` re-seeds `next_scheduled_time` so the new schedule takes effect from now. `Suspend`/`Unsuspend` toggle the job; the destroy path is gated by `AllowDestroy`, with the controller verb named `Delete`.

**Consequences.** Startup registration is safe to run on every boot, and config drift surfaces as an error rather than a silent overwrite. Schedule changes never fire off the old schedule's stale due time. Destruction stays a deliberate, opted-into operation.
