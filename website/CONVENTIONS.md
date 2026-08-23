# Website conventions

Rules for the website/ tree. Violations are bugs, not style nits.

The root CONVENTIONS.md stays authoritative for everything
cross-cutting; these sections bind here by reference and are never
restated: ## Naming & terminology, ## Vocabulary, ## Structure,
## Comments, ## Documentation, and the prose grammar of ## Errors and
## Logging for all UI copy (console errors, empty states, search
results). This file holds only what is specific to the frontend
toolchain.

## Stack

The settled set -- a new tool enters only by replacing a row, never
beside one:

- Astro: static output, content collections, ClientRouter view
  transitions. No Starlight.
- Svelte 5 is the ONE island framework; .astro files are page
  scaffolding and layouts only -- anything worth a Storybook story is
  a Svelte component.
- CodeMirror 6 (+ lang-sql) and PGlite power the SQL console.
- Pagefind is site search.
- Vitest (pure functions), Playwright (flows), Storybook
  (svelte-vite + addon-svelte-csf) are the test surface.

## Dependencies

- The root dependency rule applies unchanged: minimal deps, vendor
  battle-tested code when a whole package is not earned. npm makes
  sprawl cheap -- every new package needs the same justification as a
  Go dep, and a utility that a rune module or twenty lines of CSS can
  carry is written, not installed.

## TypeScript

- `astro/tsconfigs/strictest` + `verbatimModuleSyntax`; `astro check`
  and `svelte-check` run in the build -- the dev server type-checks
  nothing.
- `any` is a lint error (`@typescript-eslint/no-explicit-any`); a
  genuine unknown is `unknown`, narrowed at the boundary.
- Every component declares one named Props type; every field has an
  explicit type and is required -- no `?` fields, no fallback inside
  the component: the caller states every fact, defaults included
  (`width={18}` at the call site; a story file's shared values live
  in its meta `args`). Expected absence of a fact is an explicit
  `| null` union -- the caller passes `null` and the compiler forces
  both branches. Never `?: T | null`, and never an empty-value
  stand-in for null.
- Extending a platform type (`HTMLButtonAttributes & {...}`) is the
  one sanctioned answer to attribute explosion -- never a loose index
  signature.
- Every console query gets a named row type listing the columns it
  returns -- the TS sibling of the datastore `db:`-tagged row
  structs. PGlite results are typed at the query call, nowhere else.

## Components

- One folder per component under `src/components/<name>/`,
  kebab-case files: `thread-row/thread-row.svelte` +
  `thread-row.css` + `thread-row.stories.svelte`, plus `types.ts`
  and a `<name>.svelte.ts` state module when the component is
  stateful. No index.ts barrels -- callers import the file directly.
- A component used by one route lives beside it in that route's
  `_components/` directory, not in `src/components/` (the placement
  law).
- A page's data logic lives in a route-local `_<page>/` folder,
  one concern per file: `model.ts` holds the typed row shapes the
  page renders (the sibling of a datastore's model.go), the hand-
  curated content that feeds them gets its own named file
  (`boards.ts`), and the functions that build rows from the
  collection live in `rows.ts`. The `.astro` frontmatter is
  fetch-call-render only.
- A component's styles live in its sibling `<name>.css`, reached ONLY
  through the one-line `<style src="./<name>.css"></style>` tag --
  svelte-preprocess inlines the file before the compiler runs, so
  scoping is identical to an inline block. A `.css` file is never
  imported (the ESLint no-restricted-imports guard bans it -- an
  imported stylesheet is global css in disguise); the global
  stylesheet's sanctioned import sites are the layout and the
  Storybook preview. An oversized style file splits the component,
  never escapes scoping. Astro cannot scope an external stylesheet
  (a css import in `.astro` is global), so a page carries NO
  `<style>` block at all -- page styling belongs to components, and
  the shared content frame to the layout. The layout alone keeps
  one small scoped block.
- Stateful vs presentational is a file split: logic is a runes class
  in `<name>.svelte.ts`, the .svelte file is a thin renderer.
- `$derived` over `$effect` -- `$effect` is for real side effects
  (DOM, storage), never for deriving state (lint-enforced,
  `svelte/prefer-writable-derived`).
- Callback props, never event dispatchers; snippets, never slots;
  `onclick=`, never `on:click`.
- Shared state is a runes module (`.svelte.ts`) imported by whoever
  needs it; no store library. localStorage reads and writes live in
  the owning state module, wrapped so absence or denial falls back to
  defaults.

## Islands & loading

- Static is the default. Every island names its trigger at the use
  site -- `client:visible` or `client:idle`; `client:load` is banned.
- Island props are serializable values against the component's Props
  type -- functions never cross the boundary.
- Heavy chunks (PGlite wasm, CodeMirror) load only through dynamic
  import inside their island, never in the initial payload. The
  console's shell is server-rendered -- example SQL as build-time
  highlighted text over real seeded result rows -- and the editor
  swaps in sized identically.
- A Playwright test asserts the homepage's initial JS stays under the
  declared ceiling; a chunk that regresses it is a failing build.

## CSS

Vanilla CSS only -- native nesting, custom properties; no
preprocessor, no utility framework, no third-party token pack.

- One global stylesheet opens with
  `@layer reset, tokens, base, compositions, utilities;` and is the
  only global CSS. Scoped component styles stay unlayered, so they
  always win over the layers.
- Tokens are two tiers, one way: primitives (the raw design sheet)
  feed semantic names; components consume semantic tokens ONLY.
  Token names spell out per the root naming rules.
- Raw values -- hex colors, font stacks, z-index integers, magic
  spacing -- exist only in the tokens layer (stylelint +
  declaration-strict-value enforces it). z-index and spacing are
  closed token scales, not ad-hoc integers.
- base styles the classless HTML that rendered MDX produces.
  compositions are the hand-written layout primitives (.flow,
  .cluster, ...) and carry no color or type. A utility is generated
  from a token and exists only once three call sites need it; until
  then the declaration stays in the block.
- A component's scoped style block stays under ~80 lines; states are
  `data-` attributes (`[data-folder='new']`), never modifier
  classes.
- `:global()` is a sanctioned-crossing list: the base layer's MDX
  styling and view-transition names. Anywhere else it is a bug.
- A theme is a tokens-layer redefinition; a component never branches
  on theme. All motion sits behind `prefers-reduced-motion`.

## Content

- Frontmatter is enforced by the collection's zod schema and nothing
  else; enum-shaped fields are `z.enum`; every field carries
  `.describe()` prose.
- The body starts at H2 (the H1 is the frontmatter title); heading
  levels never skip (remark-lint enforces both).
- Markdown first: a component appears in MDX only from the
  whitelisted set (aside, tabs, the console); anything prose can
  carry stays prose.
- Each page does ONE job -- tutorial, how-to, reference, or
  explanation; a guide that starts explaining links to the concept
  page instead of drifting.
- Vale runs in CI with the Google developer-docs style plus the
  Vulkan style; the Vulkan substitution rule mirrors the root
  ## Vocabulary table, and a new vocabulary row updates the Vale rule
  in the same change. Code blocks are exempt (IgnoredScopes = code).

## Storybook

- `.stories.svelte` (addon-svelte-csf) is the ONE story format,
  co-located with its component.
- Every supported state is a story -- the story list is the
  component's done-checklist; story count tracks states, never usage
  sites.
- Everything editable is an arg; titles follow `Board/<Name>`.

## Data from the library

- The compat widget reads a build-time JSON export of the migration
  registry -- the gate logic is never reimplemented in TS.
- An embedded SQL literal keeps its `-- vulkan: <package>.<method>`
  first line and is drift-checked against the Go source in CI; a
  diverged literal is a failing build.
- Every number on the site is real or absent -- no invented member
  counts, view counts, or activity. Performance claims follow the
  root ## Documentation rule: benchmark records only.

## Verification

One command runs the whole floor (the site's sibling of
`just verify`): Prettier (svelte + astro plugins), ESLint (svelte +
astro + typescript-eslint recommended), stylelint, `astro check` +
`svelte-check`, Vale + remark-lint, Vitest, Playwright. A rule in
this file that a tool can check runs as a check -- review carries
only what machines cannot.
