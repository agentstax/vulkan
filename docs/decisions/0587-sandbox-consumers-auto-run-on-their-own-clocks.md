---
status: accepted
date: 2026-08-24
phase: pre-v1
---

# 0587 — sandbox consumers auto-run on their own clocks

## Context

[0586] gave each card a Run button: one click, one claim, one handler call,
one commit. That taught the step but misrepresented the thing — a consumer
instance in the library polls, and a card that only moves when clicked reads
as though claiming were a manual verb the caller invokes. The reader also had
to discover the button before the sandbox did anything at all.

The user asked for the manual button to be replaced outright, not joined, by
an auto-run toggle that is on by default. This supersedes the "no timer" half
of the step-by-step point settled 2026-08-24.

## Decision

A per-card auto-run toggle, on for every consumer the moment it exists —
seeded, added, or rebuilt by Reset.

- Roughly one run a second, `1000ms ± 150ms`. The jitter is the point:
  consumer instances poll on their own clocks, and cards started in the same
  moment would otherwise stay in lockstep forever, which reads as one timer
  driving all of them.
- The next run is scheduled after the previous one resolves — never
  `setInterval`, which would stack a second claim on a slow one.
- The timers live in `AutoRunner`, a plain-TS class in the sandbox folder:
  it holds no reactive state (the flag the card renders lives on `Consumer`),
  so it stays out of a `.svelte.ts` runes module, which vitest cannot load
  here. It is covered by fake-timer tests, including the stop-mid-run case
  the reschedule tail makes possible.
- `ChromeButton` gained `pressed: boolean | null` rather than a parallel
  toggle component beside it. `null` is a button that does its action once and
  renders no `aria-pressed` at all — which is the correct ARIA for a
  non-toggle, not a stand-in for `false`.
- The on-state styles off `[aria-pressed='true']`, not a `data-` attribute:
  the button already carries the state, and two attributes for one fact is two
  places to keep true. Muted amber — the board's new-or-act colour at ground
  strength, with the dim lacquer so it reads sunken; a grid of saturated ones
  would read as a row of warnings.
- The global in-flight lock is gone. `disabled` on a card now means the
  database is unavailable (booting or being rebuilt), never that some other
  card is mid-claim.
- A run that throws turns its own auto-run off. A clock firing against a
  database that never came up would rewrite the same error every second and
  never get past it; the unpressed toggle is the statement that it stopped.
- Reset stops every clock before dropping the database handle, and starts the
  seeded card's again after the rebuild.

## Consequences

- The sandbox is live on arrival: the eight seeded messages drain within about
  eight seconds of the island mounting, and a produced message is claimed
  inside the next second with no click.
- Nothing steps a consumer by hand any more. Turning auto-run off freezes a
  consumer where its last run left it, which is how a reader holds one group
  still while another drains — but single-stepping is no longer available.
- PGlite serializes on its own mutex and `claim` runs in a transaction, so
  several clocks firing at once queue rather than interleave. Disjoint ranges
  off one cursor are now demonstrated by two cards running unattended instead
  of by alternating clicks.
- The card's status bar still reports only the last run, so a caught-up
  consumer sits on `caught up · nothing to claim` while its clock keeps
  turning. The clock's visible evidence is the toggle, not the status line.
