---
status: accepted
date: 2026-08-23
phase: pre-v1
---

# 0582 — site stack: Astro without Starlight, Svelte 5 islands

## Context

The board design ([0583]) replaces every surface Starlight provides —
chrome, nav, sidebar, search UI, asides, code fences, splash. Keeping
Starlight underneath would mean fighting its layout and theme for every
component, and its bundled MDX/expressive-code pin the versions.

The site also needed rules of its own: nothing in the root
CONVENTIONS.md speaks to components, CSS, or prose linting.

## Decision

Astro stays; Starlight goes. What survives is Astro-core: the content
collections (glob loader plus this project's own zod schema, replacing
`docsSchema`) and Pagefind, both framework-agnostic. `@astrojs/mdx`
replaces Starlight's bundled MDX. The glob loader's `generateId` keeps
each file's casing, so the deployed URLs (`/errors/VK0005/`) are
unchanged by the swap and no redirects are owed.

Svelte 5 is the one island framework — no second one, and no store
library: shared client state is a `<name>-state.svelte.ts` runes module.
CSS is vanilla CUBE-style: `@layer` reset/tokens/base/compositions/
utilities, every component a folder holding `<name>.svelte`, a sibling
`<name>.css` inlined by svelte-preprocess so scoping is unchanged, and
`<name>.stories.svelte`. Anything worth a story is a Svelte component;
`.astro` files are page scaffolding only. Page-derived facts live in a
route-local `_<page>/` directory, never in the page frontmatter.

Initial load is a rule, not a habit: `client:load` is banned, every
island declares `client:visible` or `client:idle`, and PGlite and
CodeMirror are dynamic imports so they never enter the first payload.

The frontend rules live in `website/CONVENTIONS.md`, loaded through
`website/CLAUDE.md`, binding the root file's sections by reference.
`just site-verify` (`npm run verify`) is their enforcement: prettier,
eslint, stylelint with declaration-strict-value, `astro check` at
tsconfig strictest, svelte-check, remark-lint, Vale enforcing the
## Vocabulary registry, vitest.

## Consequences

- The whole site (80 pages, 72 thread pages indexed by Pagefind) renders
  through this project's own layouts and components.
- Astro 7 (rolldown) landed on top with no code changes.
- Version pins are load-bearing: Storybook needs `viteFinal` to prepend
  vite-plugin-svelte, because an Astro project has no `vite.config.ts`
  for its builder to inherit.
- Still open: a spacing token scale (declared in website/CONVENTIONS.md,
  not yet defined or swept, so stylelint cannot enforce it) and a
  Playwright test asserting the homepage's initial JS byte ceiling.
