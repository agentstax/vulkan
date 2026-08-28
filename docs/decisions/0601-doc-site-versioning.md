---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0601 — doc site versioning: one live site, frozen deployments per version

## Context

The machinery for versioned docs had to exist before the first release
that contradicts old pages. The field has three models: snapshot-in-source
(Docusaurus — every version's markdown copied into the repo, all rebuilt
every build), build-per-git-ref (Read the Docs, mike), and a single live
site with each old version deployed once and never rebuilt (React, Vue,
Vite, Svelte, Tailwind). The site is hand-rolled Astro with hand-curated
nav and one flat Pagefind index, so in-source copies would touch every
route, layout, and the search markup; the deploy is already a direct
`wrangler pages deploy`, and Cloudflare Pages gives any non-production
`--branch` a permanent alias origin.

## Decision

- The apex serves exactly one version. At a release checkpoint the same
  build deploys twice: `just site-deploy` (live) and `just site-freeze
  <slug>` (`<slug>.vulkan-5ss.pages.dev`), never deployed to again.
- A build can never know about versions released after it, so no build
  carries the version list. `public/versions.json` on the live origin is
  the one registry (`latest` + version/url rows); every deployment —
  version aliases included — fetches it at read time. `public/_headers`
  grants `Access-Control-Allow-Origin: *` on that one path so the alias
  origins can read it.
- A build carries only its own identity: `docsVersion` in `src/site.ts`,
  bumped in the same change that adds the release's versions.json row.
  Pre-release the stamp is `main` — no invented version numbers.
- The visit bar island (tracked-visit-bar, `client:idle` in BoardLayout)
  fetches the registry and renders version-select at its left edge: a
  select of every version with latest labeled, switching to the same path
  on the chosen origin. When the build's stamp is not the registry's
  latest, the bar wears the sticky-row tokens and says "You are reading
  the {version} docs" with a same-path link to latest — so an old
  deployment starts announcing itself the moment the live registry
  moves, without being redeployed; unreachable, only the stamp shows.
- Every page carries `<link rel="canonical">` naming the live origin;
  Cloudflare already serves `x-robots-tag: noindex` on alias domains
  (verified), so old versions never compete in search.

## Consequences

- Old versions cost nothing: no copies in the repo, no build growth, no
  backport edits. A fix to an old version would mean checking out its
  release commit — accepted; narrative docs version per breaking release,
  not per fix.
- Cross-version search does not exist; each deployment keeps its own
  Pagefind index. Accepted as correct — hits always match the docs read.
- Rejected: in-source snapshot copies (a permanent curation tax across
  nav, routes, and search for the one benefit — old docs inheriting
  restyles — nobody asked for) and build-per-ref machinery (the Pages
  branch alias already is that, for free).
