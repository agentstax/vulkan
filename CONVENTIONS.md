# Conventions

Codebase-wide rules. Violations are bugs, not style nits.

each func param has explicit type, never combined

## Dependencies

- Main module deps stay minimal: std lib + pgx + google/uuid + x/sync. Never
  `go get` a new dep for domain logic.
- When battle-tested code exists for a problem (e.g. cron parsing), VENDOR it:
  copy the needed source files + their tests + license verbatim into the owning
  package with provenance headers, take only the parts needed, keep local diffs
  to marked one-liners. Hand-roll only when nothing battle-tested fits.
- The nested modules are the sanctioned exception -- separate modules, not
  the library: cmd/vulkan (cobra/fang/lipgloss) and otelvulkan
  (otel/prometheus).

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
- A function returns one value plus an error; three return types are the
  sign it is doing too much. First ask whether the extra value has a real
  consumer -- usually another verb already owns that fact, so delete it,
  don't wrap it. Only when callers genuinely need both does the pair become
  a named result struct (with its New<Struct> constructor). The comma-ok
  bool for expected absence is the exception.

## Package layout

Three layers per domain (template: worker, topic):

- `pkg/<x>` -- vocabulary only: pure read-models, consts, error sentinels.
  Imports ~common only. No constructors for read-models, no Config types, no
  fields without production readers.
- `pkg/<x>/controller` -- the only door to persistence: all public verbs, ALL
  input validation, `to*` adapters, schema asserts. Files: `<x>_config.go`,
  `controller_config.go`.
- `pkg/<x>/controller/datastore` -- all SQL; trusts inputs, no re-validation.
  Table-exact `*Data` structs live in `model.go`, never beside the query that
  returns them. An enum type travels with its const block.
- Import arrows point strictly downward.
- Error sentinels are declared in the owning domain's `pkg/<x>` vocabulary
  (`errors.go`); a sentinel shared across different stacks lives in
  `pkg/common`. Whichever layer detects the condition raises it -- admin
  for guards it composes, a datastore for facts its own query discovers.
- A config file is named for the struct it declares, never bare `config.go` --
  `<x>_config.go`, `controller_config.go`, `datastore_config.go`. A package
  that grows a second config gets a second file rather than a shared one.

## Datastores

- Every public datastore method is EXACTLY a `DatastoreRetry.Wrap` around a
  same-named private method -- all SQL, scanning, and result shaping live in
  the private, even for one-query reads.
- Every scan-destination row struct tags each field `db:"column"` with the
  column or alias its query returns -- the tag is the field's column
  contract regardless of scan style. Write shapes, derived outcomes, and
  composite aggregates carry no tags.
- Datastore methods are dumb resource verbs on the domain's own tables --
  get, register, delete, list. A caller-shaped read (a guard, a health
  check) is composed above the datastore, in the controller or admin, from
  those verbs or from the domain that already owns the fact (metrics
  snapshots, worker liveness). A datastore read that re-derives another
  mechanism's fact -- or reaches another domain's tables outside a
  transaction -- is a second read path, not a convenience.
- File content order is pair-by-pair: each public immediately followed by its
  same-named private, then the next pair; deeper helpers a private calls
  follow the pair that uses them. Never all publics then all privates.
- Method bodies are a linear sequence of named calls -- no inline shaping
  wads. `any` values go straight to pgx as query args (driver encodes JSONB;
  never hand-call json.Marshal); nil/empty shaping happens SQL-side
  (`NULLIF`, `COALESCE`).
- A `*common.Owner` is never nil -- no nil-safe receivers. A param nothing
  can populate yet gets deleted, not nil-tolerated.
- Controllers own verbs, not tables. A datastore transaction contains every
  statement its operation needs, inline, even on tables another domain
  primarily manages.
- A transaction never crosses a package boundary -- no exported tx-taking
  methods, no `Querier`/`pgx.Tx` in any public signature. If an operation
  seems to need two controllers, it is one operation with a wrong home: pick
  the owner whose invariant the transaction protects.
  The ONE sanctioned crossing is the produce-transaction seam:
  `producer.InTransaction` hands its closure the producer `Tx`, and a method
  built to run inside that closure takes the `Tx` (when it runs a
  ProduceFunc) or `q datastore.Querier` (when it only runs statements).
- `datastore.Querier` is the one statement seam: Exec / Query / QueryRow /
  SendBatch / CopyFrom -- what pool, conn, and tx can all do, minus
  transaction control. A private that runs inside a boundary it doesn't own
  takes `q datastore.Querier`; `pgx.Tx` appears only as a local in the
  private that owns Begin/Commit and in the adapter that builds the producer
  `Tx`. No Beginner/pool interface, no wrapping of Rows/Row/CommandTag --
  pgx's own result types pass through. `*pgxpool.Conn` stays concrete in
  migrate: the advisory lock pins a session, and the concrete type is that
  contract.

## Constructors & configs

- Every new struct gets `New<Struct>(required params) (*Struct, error)` and
  call sites use it -- never bare literals. Exception: vocabulary read-models
  built only by controller adapters get no constructor.
- Required params inline in the signature; optional ones in a slim sparse
  Config struct. Never pass a whole data struct for a couple of fields.
- Param order is primary collaborator first, ambient last: the dep the struct
  is *about* leads, then its remaining deps, then `cfg`, and a bare
  `log common.Logger` always trails (prefer `cfg.Logger` over a bare param).
  A logger in the first position is the tell that a signature was copied from
  somewhere else -- readers scan position 1 for what the thing operates on.
- No functional-options pattern. Every config struct: exported
  `WithDefaults()` (fills zero fields, mutates + returns receiver) then
  `Validate()` (validates the RESOLVED config), both in the config's own file
  (see Package layout). Constructors nil-check required deps, then
  default+validate their own config, returning errors all the way through.

## Pointers & receivers

- Pointer receivers on everything a constructor builds -- controllers,
  datastores, configs, workers. Value receivers only on small immutable
  vocabulary types (Owner's accessors, a string enum's `Validate`). Never
  mix receiver kinds on one type: pointer-receiver methods are absent from
  the value's method set, so `T` and `*T` satisfy interfaces differently
  and a copied value silently loses the mutating methods.
- A pointer param states that the callee mutates or shares the value --
  never a performance reflex. Flat row-shaped structs and stdlib values
  (`time.Time`, `uuid.UUID`) pass by value; nested read-models travel as
  pointers end to end. Slices and maps are already reference-backed, so
  element types are values (`[]Data` out of datastores) unless the element
  is itself pointer-classified (`[]*Topic`); never `[]*T` to make room for
  nil entries.
- Config structs are passed as `*Config` while being resolved --
  `WithDefaults()` mutates in place. A long-lived instance stores a value
  copy once resolved, so caller mutations after construction change nothing.
- Constructors return `(*Struct, error)`, nil on error: a caller that
  ignores the error panics at first use with a stack trace, instead of
  proceeding on a zero value that looks meaningful.
- Outside the error path a pointer is never nil (`*common.Owner` is the
  template): no nil-safe receivers, no nil-means-unset params. Expected
  absence is comma-ok; a param nothing populates yet gets deleted.
- A struct holding a mutex, atomic, or connection pool is pointer-only:
  copying it copies the lock, which is a data race, not a style slip.
- A value copy is not isolation: any slice, map, or pointer field inside it
  still aliases the original's backing memory, so mutating a copy can break
  the original's invariants.
- Accept interfaces only at real seams (`common.Logger`; `Querier` stays
  private); return concrete `(*Struct, error)`. Never return a concrete
  pointer through an interface-typed return -- a typed nil stored in an
  interface compares non-nil, so every downstream nil guard lies.

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
- That ban covers CTEs and subqueries, which have no scan destination to break:
  a `SELECT *` there silently widens with the table, so a later column lands in
  a `FOR UPDATE` row image or a join nobody re-read. Name the columns the CTE's
  own body reads and nothing more -- the list is then the CTE's contract, and a
  new column reaches it only when someone adds it deliberately.
- More than 3 selected columns go one per line. A wrapped column list hides
  which columns moved in a diff and makes the scan destinations hard to line
  up against. 3 or fewer stay on the `SELECT` line.
- Inline `--` comments right-align as a group, to the furthest-out one. A
  ragged comment column reads as unrelated notes; an aligned one reads as the
  table it is.
- No CHECK-constrained enums: a value-set CHECK makes every new value a
  migration. An enum-shaped TEXT column lists its values in an inline comment
  (`-- 'installed' | 'waiting'`); Go typing and validation are the only
  enforcement. Structural constraints (NOT NULL, FKs, uniqueness) stay in SQL.

## Comments

- Avoid large blocks of text, break apart using formatting or single
  statements per line
- Default is no comment. A comment earns its place only by stating a why or
  gotcha the adjacent code cannot show -- restating the code/SQL/signature
  below it, boundary/wiring narration, and design essays all get cut.
- 1-2 lines, rationale directly above the statement it explains. Parallel
  facts enumerate one per line (`condition -> outcome`).
- Every comment stands alone -- it is read cold by someone who was not in the
  discussion that produced it. Name the subject instead of opening mid-thought
  (`held for the length of a Consume call` -> `consumePermit is held for...`),
  finish the sentence (`the reclaim needs its own` -- its own what?), and never
  lean on a noun the surrounding code never introduces (`the drain rides this
  ctx` where nothing nearby is called a drain). A comment that argues against
  an alternative someone raised in review is that conversation leaking into the
  file: cut it, or state only the rule it settled on.
- Never point outside the code: no plan/phase references, no symbols that
  don't exist yet, no benchmark allusions.
- "Improve the comments" means delete first, then wordsmith survivors.

## Labs

- A lab that hand-copies a production query (EXPLAIN demos) goes silently
  stale when the real query changes -- grep labs for mirrors whenever a
  production query moves. Prefer driving the real datastore method.