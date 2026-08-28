---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0599 — the cookie notice is the site's privacy note, on its own surface

## Context

The site sets no cookies. It has no analytics, no tracker, and no third
party to send anything to; the only stored state is localStorage the
reader's own browser holds (read tracking, board style, the reload guard).
Nothing on the site said so, and a privacy page nobody opens would not
have said it either.

A consent banner is the one piece of chrome every reader has been trained
to look at. Wearing that costume to deliver a true statement puts the
fact where it will actually be read.

## Decision

A cookie notice in two acts, its own component under
`src/components/cookie-notice/`, mounted `client:idle` in BoardLayout.

- **Its own surface, never site-notice.** site-notice is the shipped
  error channel [0597]: a prank on it would teach readers to ignore the
  one banner that means something actually broke. The two never share a
  component, a state module, or a screen position — the notice sits on
  the bottom edge, site-notice on the top.
- **Act one is a faithful copy of the standard**: a "we value your
  privacy" title, the cookies-and-similar-technologies paragraph, three
  equally prominent buttons (Accept all / Reject non-essential / Manage
  preferences) and a cookie-policy control. Equal prominence is the
  compliance-vendor convention and also the joke — every one of them
  reaches the same act two, so which was pressed changes nothing.
- **Act two is the real privacy statement** under a headline drawn from
  a five-entry pool. Only the headline and the two button labels rotate;
  the statement under them is one fact and never varies.
- **Each act-two button carries two lines** — the joke the reader reads,
  and beneath it the plain action they are pressing ("star the repo",
  "dismiss"). The accessible name therefore contains the visible label
  and the real action both, so the bit costs nothing in clarity.
- **First visit, one answer per browser**, stored in localStorage at the
  moment act one is answered. No veil, no focus trap — a real consent
  bar does not block the page, and neither does this one.

Rejected: firing on a return visit instead (funnier against the
"you last visited" bar, but most readers would never see it); a modal
veil (contradicts the costume and taxes every first read).

## Consequences

- One more localStorage key, `vulkan-board:cookie-notice`, itself named
  in act two's statement — the notice discloses its own storage.
- The statement is now load-bearing prose: a change to what the site
  stores has to change act two's paragraph in the same edit.
- Adding a punchline is an entry in `reveals.ts` and nothing else.
