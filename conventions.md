# Conventions

Codebase-wide rules. Violations are bugs, not style nits.

each func param has explicit type, never combined

## Dependencies

- Main module deps stay minimal: std lib + pgx + google/uuid + otel/prometheus
  + x/sync. Never `go get` a new dep for domain logic.
- When battle-tested code exists for a problem (e.g. cron parsing), VENDOR it:
  copy the needed source files + their tests + license verbatim into the owning
  package with provenance headers, take only the parts needed, keep local diffs
  to marked one-liners. Hand-roll only when nothing battle-tested fits.
- The CLI's nested module (cobra/fang/lipgloss) is the sanctioned exception --
  separate module, not the library.

## Naming & terminology

- Never coin shorthand for a mechanism -- not in code, comments, or names.
  Describe things as the row/column/status/action they literally are
  (banned examples: park, give-back, IOU, slot, ack/nack, settle, cede).
- Name a new method after the verb the codebase already uses for the same
  concept (Record*/Claim* pairs), not an outside domain's jargon.
- Design-round vocabulary (analogies, research jargon like "exclusive arc")
  is for discussion only -- translate back to the codebase's own nouns before
  writing names or comments.
- Use the plainest verb for the action -- `put`, `set`, `read`, `write`,
  `send`, `delete`. A more vivid verb (`stamp`, `hydrate`, `wire up`,
  `bake in`, `ferry`, `hand off`, `thread through`) must carry information
  the plain one doesn't; otherwise it is decoration the reader has to
  translate back. Applies to prose in comments, not just identifiers.
- When a bad name turns out to be an established pattern, fix every
  occurrence, not just the new one.
- Spell variable and param names out -- `message`, `instance`, `duration`,
  `policy`, `attempt`, `token`, `owner`. Don't truncate (`msg`, `inst`, `dur`,
  `pol`, `att`, `tok`, `own`) and don't reduce to initials (`m`, `i`, `p`).
  Vowel-dropping and first-syllable clipping are the same offense: the reader
  should never have to expand a name to know what it holds.
- The ONLY sanctioned abbreviations are the established house set -- `ctx`,
  `tx`, `cfg`, `ds`, `err`, `id`/`ids`, `sql`, `wg`. Nothing gets added to
  this list ad hoc; if a name feels too long, the type is probably doing too
  much.
- Single letters are for loop indices and receivers only (`i`, `p`, `c`), and
  the receiver matches the type's initial. A single letter never holds a
  domain value.

## Structure

- Extend existing machinery, never build a parallel mechanism beside it.
  Start from "what one-clause change lets the existing path carry this?" An
  oversized diff for the feature's conceptual size is itself the smell.
- No new shared packages for logic both producer and consumer need -- write it
  on each side's datastore in its local style. Duplication beats abstraction.
- One established mechanism per fact -- never introduce a second read path or
  derivation for something the codebase already computes one way.
- One concept = one named home. Policy tables become a pure classify function
  returning a named-action enum + an exhaustive switch driver. Prefer the
  simplest construct that fits the mental model.

## Package layout

Three layers per domain (template: worker, topic):

- `pkg/<x>` -- vocabulary only: pure read-models, consts, error sentinels.
  Imports ~common only. No constructors for read-models, no Config types, no
  fields without production readers.
- `pkg/<x>/controller` -- the only door to persistence: all public verbs, ALL
  input validation, `to*` adapters, schema asserts. Files: `<x>_config.go`,
  `controller_config.go`.
- `pkg/<x>/controller/datastore` -- table-exact `*Data` structs + all SQL;
  trusts inputs, no re-validation.
- Import arrows point strictly downward.

## Datastores

- Every public datastore method is EXACTLY a `DatastoreRetry.Wrap` around a
  same-named private method -- all SQL, scanning, and result shaping live in
  the private, even for one-query reads.
- Method bodies are a linear sequence of named calls -- no inline shaping
  wads. `any` values go straight to pgx as query args (driver encodes JSONB;
  never hand-call json.Marshal); nil/empty shaping happens SQL-side
  (`NULLIF`, `COALESCE`).
- A `*common.Owner` is never nil -- no nil-safe receivers. A param nothing
  can populate yet gets deleted, not nil-tolerated.

## Constructors & configs

- Every new struct gets `New<Struct>(required params) (*Struct, error)` and
  call sites use it -- never bare literals. Exception: vocabulary read-models
  built only by controller adapters get no constructor.
- Required params inline in the signature; optional ones in a slim sparse
  Config struct. Never pass a whole data struct for a couple of fields.
- Param order is primary collaborator first, ambient last: the dep the struct
  is *about* leads, then its remaining deps, then `cfg`, and a bare
  `log logger.Logger` always trails (prefer `cfg.Logger` over a bare param).
  A logger in the first position is the tell that a signature was copied from
  somewhere else -- readers scan position 1 for what the thing operates on.
- No functional-options pattern. Every config struct: exported
  `WithDefaults()` (fills zero fields, mutates + returns receiver) then
  `Validate()` (validates the RESOLVED config), both in the package's own
  `config.go`. Constructors nil-check required deps, then default+validate
  their own config, returning errors all the way through.

## Migrations

- Pre-v1, every schema change edits the baseline `CREATE TABLE` DDL in place
  -- no ALTER/DROP trail. Removed tables' DDL is deleted outright. Verify by
  drop+recreate of the dev DB.

## SQL

- Never `SELECT *` -- always name columns explicitly. A column ADD must be
  invisible to live binaries built before the column existed; `SELECT *` makes
  even additive schema changes breaking (pgx errors when the field count no
  longer matches the scan destination count). Explicit column lists are what
  keep adds non-breaking, leaving column removal as the only change that needs
  the two-release expand/contract dance.
- More than 3 selected columns go one per line. A wrapped column list hides
  which columns moved in a diff and makes the scan destinations hard to line
  up against. 3 or fewer stay on the `SELECT` line.
- Inline `--` comments right-align as a group, to the furthest-out one. A
  ragged comment column reads as unrelated notes; an aligned one reads as the
  table it is.

## Comments

- Avoid large blocks of text, break apart using formatting or single
  statements per line
- Default is no comment. A comment earns its place only by stating a why or
  gotcha the adjacent code cannot show -- restating the code/SQL/signature
  below it, boundary/wiring narration, and design essays all get cut.
- 1-2 lines, rationale directly above the statement it explains. Parallel
  facts enumerate one per line (`condition -> outcome`).
- Never point outside the code: no plan/phase references, no symbols that
  don't exist yet, no benchmark allusions.
- "Improve the comments" means delete first, then wordsmith survivors.

## Labs

- A lab that hand-copies a production query (EXPLAIN demos) goes silently
  stale when the real query changes -- grep labs for mirrors whenever a
  production query moves. Prefer driving the real datastore method.