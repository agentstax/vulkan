---
status: superseded
date: 2026-08-27
phase: pre-v1
---

Superseded by [0604], same day: the leak was located (the router's
unhandled `ready` promise) and caught at the source instead; the net
filter below was removed.

# 0603 — the page-failure net ignores browser cancellations

## Context

Narrows [0597]. The mobile pass [0602] put a phone in front of the
site for the first time, and the first navigation off the homepage
raised the failure banner with "Transition was aborted because of
invalid state". The chain: the browser aborts an in-flight view
transition on any mid-transition viewport resize — on a phone the URL
bar collapsing or expanding is one — and the router's un-awaited
transition promise rejects with a DOMException. The navigation itself
completes; the new page is on screen. The tier-3 net forwarded every
unhandled rejection to the banner, so a cosmetic cross-fade that did
not finish was reported to the reader as a page failure.

## Decision

- The unhandledrejection listener in
  src/state/site-notice.svelte.ts skips a rejection whose reason is a
  DOMException named `AbortError` or `InvalidStateError` — the
  platform cancelling scheduled work, not the page failing. Every
  other rejection still reaches the banner.
- The branch is on the exception's name — the DOMException sibling of
  errors.Is — never on its message text.
- The rejection stays visible in the console (no preventDefault): the
  net's job is what the reader sees, not what a developer's console
  records.

## Consequences

- A real fault that surfaces as one of these two names through an
  un-awaited promise is no longer bannered. Accepted: the site's own
  async work reports through tiers 1 and 2, and an unhandled
  DOMException with these names at window level is platform noise in
  practice.
- Rejected: filtering on the rejection's message text (wording is the
  browser's to change), and timing heuristics tying the rejection to
  a recent swap (a patch-up where a classification exists).
