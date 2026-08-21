# 0564 -- Repeated Warn/Error suppression in the logging pipeline

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

A down database turns one outage into identical Warn/Error lines from
every worker on every tick -- the renewal heartbeat alone warns every
InstanceTTL/2 per claimed row. [0558] made messages static, so identical
repeats are cheap to detect. Precedent: zap's sampler core, Logback's
DuplicateMessageFilter, Temporal poll-failure dedup, FoundationDB
SuppressedEventCount.

## Decision

- Suppression is the suppressHandler stage of the logging pipeline
  ([0565]), its state held per long-lived instance -- all the instance's
  workers share it, so a fleet-wide repeat collapses to one line plus a
  count.
- Key = (level, message); Warn and Error only -- Debug/Info are volume
  by design and already filtered. Level in the key means a Warn->Error
  escalation always emits its first Error immediately.
- First occurrence passes; repeats inside the window are dropped and
  counted; the next emission of the same key after the window carries
  suppressed_count and resets. No flush timer: a repeat run that ends
  silently loses its tail count (FoundationDB accepts the same loss)
  rather than running a goroutine inside the logging path.
- The window is a package const (1 minute), no config knob -- promoted
  to a config field only on demand.
- The stage sits after capture and before drain: the debug ring holds
  every record including suppressed ones, and a suppressed repeat Error
  leaves the held narration for the next emitted one.
- The renewal heartbeat stays a repeating Warn (recovery is another
  worker claiming the row); the tick runner's Warn->Error curve is
  untouched.

## Consequences

- suppressed_count joins the attr registry in CONVENTIONS.md.
- The emitted line's attrs are one representative occurrence; per-worker
  attrs of suppressed repeats are not aggregated.
