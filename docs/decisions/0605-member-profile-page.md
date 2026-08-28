---
status: accepted
date: 2026-08-28
phase: pre-v1
---

# 0605 — the member profile page

## Context

Every thread post's author column shows the same member — brandon,
Site Admin, volcano avatar — with the name styled as a link that went
nowhere. The board fiction says clicking a member opens their profile,
and the profile wanted a joke: the Stanley Parable loading screen's
"The end is never the end is never the end…", which is also this
project's roadmap in one sentence. In-place easter eggs and a typed
animation were considered; the user chose a real profile page with
the line scrolling endlessly through a labeled strip.

## Decision

- `/members/brandon/` renders the phpBB "Viewing profile" shape: the
  thread posts' identity column (name, stars, role, avatar at 88px)
  beside a facts list, a "Personal text" strip below — SMF's name for
  a profile's free-text field, chosen over Signature (bottom-of-post
  furniture), persona fields (Occupation, Interests, Motto), and
  mechanism names (Marquee, MOTD, Ticker). thread-post's author name
  and avatar become links, href derived as `/members/{author}/`.
- Every fact on the page is real or absent (the site's own numbers
  rule): Total posts is `siteThreads(...).length` — the same count
  every thread post shows — Website is `repositoryUrl`, and Joined is
  the repo's first commit date via a new `firstCommitDate()` on the
  existing commit-log walk in helpers/last-commit-date.ts (the walk is
  newest-first, so the date left standing after the loop is the first
  commit's; no second git call). No invented location or last-visit.
- The personal text is two identical copies of "the end is never "
  repeated, in a flex row inside an overflow-hidden box, both sliding
  `translateX(-100%)` on an infinite linear animation — the second
  copy lands where the first began, so the loop restarts invisibly.
  All lowercase: an endless sentence has no start to capitalize. The
  animation sits behind `prefers-reduced-motion: no-preference`; the
  static fallback is the line cut off at the box edge. The second
  copy is `aria-hidden`.
- Two route-local components under `pages/members/_components/`
  (placement law: one route uses them), split as member-profile +
  member-personal-text by the ~80-line stylesheet rule. Route-local
  components sit outside the Storybook glob, as sandbox-post does,
  so they carry no stories.

## Consequences

- The author column's dead link is now a real one on every post,
  404 and sandbox included; any future second author gets a profile
  URL for free and a 404 until a page exists.
- The commit-log walk now serves two facts (per-file newest date,
  repo oldest date) from its one history read.
- Below 640px the identity column folds into the same header strip
  thread-post uses.
