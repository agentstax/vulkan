---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0597 — website layered error handling

## Context

The site had three deliberate error strings and no global handling. The
flagship failure was invisible: PGlite boot failure wrote a DatabaseState
`'failed'` status nothing read, so the progress overlay vanished and the
buttons re-enabled with no message. The CodeMirror chunk import had no
.catch (unhandled rejection, silently read-only box), a search throw left
"searching…" up forever, and raw exception text — `[object Object]`
included — reached panels verbatim. Research across the Svelte and Vite
docs, four design systems, NN/g, and PGlite's issue tracker settled the
mechanics.

## Decision

Four severity tiers, disruptiveness matched to scope, built as four
separate rungs (expanded in TODO.md).

- Tier 1, inline at the source: the existing
  `errorMessage: string | null` prop + `role="alert"` pattern, extended
  to every uncovered call site. `<svelte:boundary>` does not catch DOM
  handlers, timers, or async work, so call-site try/catch stays the
  workhorse.
- Tier 2, component fallback: a shared `<svelte:boundary>` failed-snippet
  component around island innards for render/effect errors, plus a
  dedicated read of the sandbox's boot-failure status. Storage denial
  degrades to in-memory plus messaging, never a dead console.
- Tier 3, ONE site-notice component fed by three window listeners
  registered once in a bundled layout script (bundled modules run once
  and survive ClientRouter swaps): 'error', 'unhandledrejection', and
  Vite's 'vite:preloadError'. Banner is the default surface; modal is
  reserved for the one must-acknowledge case — a stale chunk after a
  redeploy: reload once behind a sessionStorage guard, then the
  persistent notice. Error toasts are banned (Primer's accessibility
  case).
- Tier 4, last resort: the same notice's full-page state. Not a route —
  static output has no 500 mechanism; 404.html stays and also keeps
  Cloudflare Pages out of SPA-fallback mode.
- The message split: reader-typed SQL shows the real Postgres error
  verbatim — the console is a terminal and the error is the content.
  Site-machinery failures speak the house problem+fix grammar already
  bound by reference. Raw exception text never otherwise reaches the
  page.
- website/CONVENTIONS.md gains an ## Errors section: the tier ladder,
  when each surface is allowed, the message split, and every failure
  state is a story.

Rejected: modal as the default surface (the originating thought) — every
surveyed system reserves modal for must-acknowledge; a /broken route —
nothing on static hosting ever serves it.

## Consequences

- ~350–450 lines across the four rungs; rungs 1–2 edit the
  sandbox/database files the pending sandbox refactor wants to
  restructure, so sequencing is checked per rung.
- Failure states become story-listed states, closing the
  every-state-is-a-story gap the survey found.
