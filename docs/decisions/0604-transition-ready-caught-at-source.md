---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0604 — the transition's ready promise is caught at the source

## Context

Supersedes [0603], same day. That record made the tier-3 net skip
every unhandled DOMException named AbortError or InvalidStateError —
over-broad, since InvalidStateError is a wrong-state programming-error
class the net could now hide. Research located the actual leak and the
ecosystem's answer: Chrome skips same-document view transitions on
mobile (a live Chromium regression, issue 456078987, also behind the
devtools mobile emulator) and rejects the transition's `ready` promise
with InvalidStateError while the navigation completes; the spec marks
`ready` handled on skip but Chrome leaks it, and a CSSWG thread
(css-view-transitions-2 #13726) is weighing spec fixes. Astro's router
attaches no handler to `ready` (unfixed upstream, issue #10830 closed
unreproduced); Nuxt fixed the identical leak in its own router with an
empty catch on `finished` (PR #34515) and `ready` (PR #35537). Nobody
filters exception names at a global net.

## Decision

- listenForPageFailures registers one more listener: on
  `astro:before-swap`, `event.viewTransition.ready.catch(() => {})` —
  the Nuxt fix applied at Astro's event seam, since the router itself
  is not ours to patch. The type import from `astro:transitions/client`
  is type-only, so Storybook's svelte-vite build never sees the
  virtual module.
- The unhandledrejection net goes back to fully strict: the [0603]
  name filter is removed. A real InvalidStateError anywhere else
  reaches the banner again.
- In Astro's no-view-transition fallback, `ready` aliases the update
  promise the router already awaits in its own try/catch — the extra
  catch changes nothing there.

## Consequences

- If a leak ever surfaces through a promise this seam cannot reach
  (Astro derives an unhandled chain internally), the banner returns
  and says so — preferred over a silent global filter.
- Worth filing upstream on Astro: their router should catch `ready`
  as Nuxt's does; this listener becomes deletable when it does, or
  when Chrome ships the spec's mark-as-handled on this path.
