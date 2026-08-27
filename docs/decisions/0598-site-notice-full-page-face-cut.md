---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0598 — the site notice's full-page face is cut until something needs it

## Context

[0597] settled four error-handling tiers for the doc site, tier 4 being
the site notice's full-page state as the last resort. Building tier 3
showed the trigger problem: every page's shell is prerendered, so the
prose renders and stays readable no matter what client-side JS does —
there is no detectable "nothing below works" signal on this site. Every
candidate heuristic (chunk failure during initial load, error volume)
would cover readable content with an opaque overlay, the one thing the
surveyed design systems say the full-page tier must not do.

## Decision

The site notice ships with two faces — banner and modal — and no
full-page state. It was built, storied, and then cut in the same round
rather than left reachable with no caller.

- Tier 4's role today is carried by the modal: the reload it asks for is
  the strongest remedy a static page has.
- Reviving the face needs a real trigger first — a page whose usable
  content genuinely depends on JS, or a failure class that provably
  blanks the shell. The cut markup was ~15 lines; rebuilding it is
  cheaper than carrying it dead.

[0597] otherwise stands unchanged.

## Consequences

- SiteNoticeKind is 'banner' | 'modal'; no dead 'page' branch to test or
  story.
- The rung-4 conventions section describes a two-face notice; if a
  full-page face returns it re-enters through a new record.
