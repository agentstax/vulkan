---
status: accepted
date: 2026-08-24
phase: pre-v1
---

# 0585 — the claim path's snapshot gate proves on PGlite, so the sandbox can tick

## Context

The consumer-flow sandbox extends the console [0584] into the whole
produce/claim path, and its Tick control depends on machinery built for a
condition PGlite cannot create. `freshClaimMessagesWithCursor` never
advances `claimed` to the raw `MAX(id)` — an id issued by a still-open
producer transaction would be skipped forever. It advances only to a head
it can PROVE, by reading a `(head, xmax)` pair before its transaction's
first write and then requiring `pg_snapshot_xmin` to have passed that
xmax.

That proof assumes many concurrent backends. PGlite is one. If the gate
never proved, Tick would claim nothing and every consumer card's
narration would have to change — so this was settled by measurement
before any of the flow was drawn.

## Decision

Build the sandbox's tick on the real claim path unchanged. A headless
round trip against PGlite in Node — register group, produce, claim,
read, commit — proves on the first poll: snapshot pair `(head=3,
xmax=789)`, `pg_snapshot_xmin` also 789, the fresh-pair branch fires, the
cursor advances `(0, 3]`, the lease frees under its token and
`settled_head` lands at 3.

It proves structurally, not by luck. The snapshot statement is a pure
SELECT, so the transaction holds no xid when it reads xmax; PGlite's
single backend means every producer transaction has already committed by
the time the gate CTE runs, so nothing holds xmin down. The
`settled_head` fallback — the branch that carries claims when neither
pair proves — is unreachable in the browser.

## Consequences

- A produce lands in the very NEXT claim, `(1, 2]`, not a tick later. No
  card needs a "wait for another tick" caveat.
- Two claims on one group's cursor come back disjoint, `(0, 2]` then
  `(2, 3]` with `first.high == second.low` — which is exactly what the
  shared-group consumer card tells the reader.
- Caught up reads `low == high`, the same fact the Go code turns into a
  nil claim, so Tick has a defined no-op state.
- The page can explain the fence but can never show it firing: staging it
  needs a second backend holding a write open. This is the same
  one-connection limit the sandbox already discloses for contention.
- The spike's SQL was pasted from the Go sources, not extracted, and was
  deleted once it answered the question. The drift-checked extraction and
  its standing coverage ride with the tick integration step, under
  [0584]'s rule.
