---
status: accepted
date: 2026-08-20
phase: pre-v1
---

# 0562 -- Log events carry VK codes from the error registry

## Context

[0550] gave errors stable VK codes with hand-written docs pages; [0558]
made logs and errors one message system. The operator-actionable Warn/Error
log lines (reclaims, dead-letters, backstops, a stopped tick loop) had no
such pointer: an operator grepping a message found nothing stable to search
or link.

## Decision

- common.NewLogEvent(code, message, consequence) -> *LogEvent{Code,
  Message}; consequence is appended after " -- " at declaration.
- Log events share the errors' VK serial space; the registry has one home,
  common/registry.go, and is generic: one map[string]Declaration behind the
  Declaration interface (GetCode/GetKind -- Get-prefixed because Code is
  the field), one register func validating isVKCode + rejecting any
  registered code, one listRegistered[D] walker. Each kind's file holds its
  type, constructor, Declaration methods, and its typed lister
  (Errors/LogEvents) -- the registry knows no concrete kind.
- The declaration boundary mirrors [0553]: a Warn/Error event
  operator-actionable enough for a docs page declares; Debug/Info never.
- Declarations live in the owning vocabulary package's logs.go
  (consumergroup, worker, cron) or beside the datastore's errors.go when
  the event is package-local (producer create-ahead).
- Call sites log the declaration's Message with `"code", Event.Code` as
  the first attr pair; the message stays static, the code is the pointer.
- 12 declarations, VK0026-VK0037; two dead-letter shapes stay two codes
  (per-batch commit vs per-delivery outcome -- one code = one message).
- Docs pages stay on the one /errors/ path; `vulkan explain` lists and
  renders both kinds. tools/conventions walks cover event messages
  (banned words) and NewLogEvent codes (registry completeness).
- No Level field on LogEvent -- the call site owns the level; pages state
  it by hand.

## Consequences

- Operators grep a code, not wording; message text stays free to improve.
- Start-line snapshot rule finished: consumer start line carries
  message_timeout, shutdown_timeout, batch_limit; config-fact keys spell
  their config field snake_cased.
