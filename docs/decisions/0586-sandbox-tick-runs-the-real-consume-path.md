---
status: accepted
date: 2026-08-24
phase: pre-v1
---

# 0586 — the sandbox's Tick runs the real consume path, and drift is counted per verb

## Context

[0584] made the console's SQL the library's own, extracted byte-exact and
counted both ways against the Go file it came from. The sandbox's Tick
needed the consume path under that same rule, and two things did not fit.

First, counting per FILE assumes the site mirrors whole files.
`group.go` also holds `deleteGroup` and `commit.go` also holds
`partialCommit` — verbs the sandbox has no caller for. Under a per-file
count, extracting the claim path meant extracting five statements nothing
would ever run.

Second, "one claim, one handler call, one commit" left open what the
handler is, and therefore whether commit writes anything at all.

## Decision

Count drift per `-- vulkan: <owner>` tag, not per file. Nine owners are
mirrored now; a statement added to one of them still fails the test, and a
verb the sandbox does not run simply has no case.

Tick runs `claim` then the handler then `commit`, with the loop between
them in the console and the statements in `database.ts`:
`consumergroup.getGroup`, `messageconsumer.freshClaimMessagesWithCursor`
(snapshot + gate), `.claimMessages`, `.readMessages`, `.commit`. The
handler succeeds on every message; its work is the line it writes to the
card. Claim limit is `BatchLimit`'s default of 1 and the lease is the
37.1s `consumer_runner` composes from the default config, so one tick is
one id and the range is legible.

## Consequences

- Commit frees the lease and writes nothing else. The demo topic's
  `delivery_log_mode` is the library default `failures`, under which a
  successful outcome is never collected — so `deliveryStatement`,
  `logStatement` and `partialCommit` have no caller and stay unextracted
  until the deferred fail-the-next-message toggle gives them one.
- `reclaimWithCursor` is skipped: commit frees the lease inside the tick
  that took it, so no lease is ever left to expire.
- Building it surfaced compaction the seed never meant to demonstrate: six
  keyed messages shared one `order-42` key, so five ticks in a row read
  `(n, n+1] · 0 messages` — correct behavior, unexplained on the page.
  The seed now gives each order its own key (`order-42`..`order-47`), so
  every seeded message reaches a claim and the keyed produce path is still
  exercised. Compaction as a thing the page teaches belongs with the
  deferred `delivery_1` panel, where superseded rows would be visible.
- Confirmed headlessly end to end: eight ticks drain the eight seeded
  messages one id at a time, a produce lands in the next claim `(8,9]`, a
  second group replays from 0 off its own cursor, and `lease_1` is empty
  after every tick.
