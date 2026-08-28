---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0600 — each consent control gets its own answer, and Accept all gets its own component

## Context

[0599] shipped the cookie notice with one act-two face whose headline was
drawn from a five-entry pool, reached identically by any of act one's four
controls. Building on it showed the draw was working against the joke: the
punchline that lands is the one that answers the button the reader actually
pressed, and a random headline throws that away. The pool also made every
control interchangeable, when their comedy is not — Accept all deserves a
consequence the other two do not.

## Decision

The pressed control is the whole input. [0599] otherwise stands: still the
site's privacy note, still its own surface, still one answer per browser.

- `answers.ts` is a `Record<ConsentButton, ConsentAnswer>` — one entry per
  control, nothing drawn at random. It is a discriminated union,
  `{ face: 'modal' } | { face: 'bar'; content: Answer }`, so the modal
  answer carries no content and the compiler refuses any read of one. That
  guard caught a stale reference the moment it was introduced.
- Three controls, not four: the Cookie policy link was cut.
- Reject non-essential and Manage preferences rewrite the bottom bar in
  place through the shared `cookie-answer` component. Accept all gets
  `accept-all-modal`, its own component with its own veil, box, copy and
  stylesheet, free to diverge as far as it wants — which it immediately
  did.
- The split is three components because one stylesheet would have run
  ~110 lines past the ~80-line rule, not because the shapes differ.

Accept all's consequence, built on that freedom:

- The page stutters first (`helpers/page-stutter`): hard opacity blinks in
  the base layer plus scroll jolts, and the modal opens the instant the
  animation ends. The shove is a SCROLL, never a transform — a transform
  on the page becomes the containing block for every `position: fixed`
  child and would fling the consent bar to the document bottom
  mid-animation. The `animationend` listener checks `event.target`, since
  a descendant's animation ending would otherwise cut the stutter short.
- A routing and account number type out a digit at a time over a dark
  `--color-hacked-veil`, with memes pasted around the box. The numbers are
  noise generated per opening, but the shape is real: 9-digit routing with
  a valid ABA checksum and a genuine Federal Reserve district prefix,
  12-digit account. A number of the wrong length reads as a prop.
- `memes.ts` declares each file once — natural size, and whether it takes
  the card border — then places it as many times as it appears.

## Consequences

- Adding a control is an entry in `answers.ts`; the Record type makes a
  missing one a compile error.
- The images load only when the modal renders, confirmed by net log: a
  plain page load requests neither. The island chunk carrying the modal's
  markup and tables is 3.4 KB gzipped on every page, accepted as small
  enough to leave static.
- The meme art is Nickelodeon's and sits in `website/public/`, so a build
  publishes it. It is local preview only and must be swapped for CC0 or
  CC BY art before the site ships; the roadmap carries that.
- [0599]'s randomized pool and its Cookie policy control no longer exist.
