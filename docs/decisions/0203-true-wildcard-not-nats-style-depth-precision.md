---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0203 — Binding patterns are true wildcards, not NATS-style depth-precise matchers

**Context.** A pattern language for `binding.pattern` had to be chosen.
NATS-style matching splits `*` (exactly one dot-delimited token) from `>`
(one-or-more trailing tokens), which can pin an exact hierarchy depth.

**Decision.** A true wildcard: `*` matches any run of characters at any
depth. `wildcardToRegex` translates a pattern to an anchored POSIX regex —
each `*` becomes greedy `.*`, literal segments are `regexp.QuoteMeta`-escaped.

**Consequences.** Depth is unpinnable: `orders.*.central1` also matches the
deeper `orders.us.high.central1`, and there is no way to say "this many
segments, not more." Traded deliberately for simplicity — nothing in the
system distinguishes "this depth" from "any deeper," so the true wildcard
covers every real need so far. The depth-precise upgrade path is documented
as follow-up work rather than built speculatively.
**Rejected:** NATS-style `*`/`>` — more expressive, but the expressiveness
had no consumer yet.
