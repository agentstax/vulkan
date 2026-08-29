---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0614 — handler outcomes by error classification

## Context

A consumerFunc returns nil or an error, and every error became a
retry: the internal outcome kinds (exception / terminal / deferred /
superseded) never reached the handler, so a declined card ran
MaxRetries times to reach `dead`, and "run me again at 02:00" could
only be spelled as a failure that burned an attempt. Playground
scenario 04 scored both as traps. River (JobCancel/JobSnooze) and
JetStream (Term/NakWithDelay) give the handler that voice as a
return value.

## Decision

- The runner classifies the handler's own error: `diagnostic.Permanent`
  -> terminal (`dead` on this attempt); `consumergroup.Delay(d)` ->
  delayed; anything else -> exception (today's path). `errors.As`
  through wrapping, so `Wrap`/`%w` chains classify the same. No new
  return type. Vulkan's own Permanent errors escaping a handler
  dead-letter the delivery — stated, not hidden.
- The user's spelling is `consumergroup.Terminal(cause)`: the diagnostic
  registry admits only `VK` codes, so users cannot declare Permanent
  conditions of their own; Terminal wraps the cause in the declared
  Permanent `ErrDeliveryTerminal` (VK0055), the mirror of Delay.
- `Delay(d)` returns a `*DelayedDelivery` carrying the duration and
  unwrapping to the declared Transient `ErrDeliveryDelayed` (VK0054).
  It writes the row `ready` with `can_run_after = now() + d`, adds one
  to the new `exception_queue.delays` column, writes a `delayed`
  delivery_log row, and does not count against `RetryPolicy.MaxRetries`.
- `attempts` stays the monotonic count of runs (the retry claim
  increments it before the run, and delivery_log is keyed on it), so
  the retry budget is read as `attempts - delays` in the claim gate,
  the kill backstop, the terminal check, and the backoff index. Giving
  the attempt back would reuse a log key.
- `RetryPolicy.MaxDelays` (0 = no cap) is the ceiling, clamped per
  message like MaxRetries: a Delay returned with `delays` already at
  the cap dead-letters with the delay's own text as `last_error`.
- `MessageMeta` gains `Attempts` (runs before this one) and `Delays`.
- Both live handler paths carry all four outcomes: messageconsumer
  (first delivery, via the range commit) and exceptionconsumer. The
  on-hold deliveryconsumer path classifies Permanent -> terminal only;
  a Delay there retries as a failure, since its claim reads no
  can_run_after.
- "snooze" joins the Vocabulary ban list.

## Consequences

- Research: NATS/SQS count a requested delay as a delivery (surprise
  dead-letters); River/Oban do not, and Oban later added a visible
  counter. Vulkan ships the counter from day one.
- A delay longer than the topic's retention is a message dropped
  before it runs; documented, not guarded.
- Pre-existing and left alone: RecordSuperseded/RecordDeferred
  decrement `attempts` and log at `attempts + 1`, the reuse this
  record avoids for delays.
