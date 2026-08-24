---
status: accepted
date: 2026-08-23
phase: pre-v1
---

# 0583 — the doc site is a phpBB-era message board

## Context

The site read like every other framework doc site, and the pages it has
are naturally forum-shaped: boards of related threads, a few pinned
starting points, and 53 error/event/metric codes that are each one
question with one accepted answer.

## Decision

The site LOOKS like a 2004 message board; it is still a static site, and
no real forum exists (a Vulkan-powered one was deferred as premature).

The mapping is literal, not decorative:

- a doc page is a thread, a board is a section, the homepage is the board
  index, `/boards/<slug>/` is the thread list, quickstart and why-vulkan
  are the stickies.
- a thread page is one post by the site, with an era author column beside
  the rendered MDX.
- a code page is a thread whose OP is the code itself — the code as
  poster, its rank the classification (permanent/transient error, log
  event at a level, metric), the composed log line as the post body. A
  declared fix renders as an ACCEPTED ANSWER post and lights a [SOLVED]
  chip; codes with no declared fix show neither, so the chrome cannot
  claim an answer that does not exist.
- read state is the amber "new" folder. It derives from ONE append-only
  page-visit log in localStorage (`{href, visitedAt}` entries, capped at
  200 with a front pop): the visit bar's stamp, whether a scope is
  unread, and the /whats-new/ list are all read off that one log, so they
  cannot disagree. Amber dims only by visiting the scope.

The visual split is the rule that keeps it readable: chrome speaks 2004
(Verdana, Trebuchet, subSilver blues, pixel icons), content speaks now
(IBM Plex Sans, Plex Mono for everything the database says). Amber means
new-or-act and nothing else. Pixel marks are rectangle-native — diagonal
silhouettes staircase at this grid and were rejected twice.

## Consequences

- Board membership has one home: `Board.threads(ids)` returns member ids
  in reading order and feeds counts, prev/next, breadcrumbs, jump-to and
  read scopes; a missing id fails the build.
- Era furniture is real or absent: Edit this page and Report this thread
  are GitHub URLs, Copy link copies the canonical URL, jump-to navigates.
  "Copy error text" was cut for having no job.
- Visited-purple links are kept as a feature; dark mode arrives later as
  a "Board style" dropdown, not a system toggle.
- The MDX dialect (`.thread-body`) and a hand-written Shiki theme carry
  code fences, so the look survived Starlight's removal ([0582]).
