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
  the receiver matches the initial of the type's FINAL word -- `d` on
  `*TopicDatastore`, `c` on `*ControllerConfig`, `i` on `*JanitorInstance`,
  `d` on every `*Definition`. A single letter never holds a domain value.

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

## File layout

- A package comment sits below the `package` clause, above the imports.
  Deliberate trade: godoc/pkg.go.dev only surface a comment placed above
  the clause -- these are for readers of the file, not the doc site.
- Top of file: only the file's free vars/consts. A const or var block owned
  by a type (an enum's values, a type's sentinels) stays glued to its type,
  never hoisted.
- Then each type's block: struct, New<Struct>, WithDefaults/Validate.
- Then methods. Files with exported methods: pair-by-pair -- each public
  immediately followed by its same-named private, then the next pair. Files
  with no exported funcs: lifecycle order -- the entry point first, then
  each step in the order the running code reaches it.
- A helper is an unexported non-receiver func -- excluding a type's
  new<Struct> constructor, which stays in its type's block -- in a file that
  otherwise holds methods. Exported free funcs are API verbs and order with
  the other publics. Helpers go at the bottom of the file, behind one
  banner:

      // ***************
      // *** HELPERS ***
      // ***************

  Methods are never helpers -- a private method stays in the flow above the
  banner. A file of only free funcs (an adapter.go) has no banner.

## Blank lines

Function bodies read as paragraphs: one blank line between steps, none
inside a step.

- A step is the group of statements one comment could name. Two groups you
  would caption separately get a blank line between them.
- Glue -- never a blank line between a statement and what consumes its
  result: the `if err != nil` check, a nil/comma-ok branch on the returned
  value, the `defer` that releases what it acquired, the Exec/QueryRow that
  runs a SQL literal declared above it.
- A mid-body comment binds downward: blank line before the comment, never
  between the comment and its statement. Exception: switch/select arms are
  table rows -- a comment captioning a case stays glued on both sides.
- A validation preamble is one step: the input guards glued, one blank line
  after the last.
- At most one consecutive blank line inside a body; none directly after `{`
  or before `}`.

## Package layout

Every package is exactly one of three kinds:

- **Infrastructure** (`common` and its subpackages `common/diagnostic`,
  `common/logging`, plus `datastore`) -- vocabulary and seams importable
  by everything.
- **Domain** -- `pkg/<noun>` vocabulary root, its `controller` and
  `controller/datastore`, and the worker packages that maintain the
  domain's tables (template: topic, consumergroup).
- **API package** (`producer`, `consumer`, `admin`, `systemmanager`) --
  constructors, configs, instances; assembles domains and workers.
  Declares no named error variables, owns no SQL, holds no vocabulary.

The seam law: anything another stack imports is a seam -- a vocabulary
root or a domain controller. What only your own tree imports nests freely
(producer keeps its controller/datastore/batcher: nothing else imports
them). The placement law: a worker package lives under the domain whose
tables it maintains, never under its assembler.

Developer tooling lives in the dev-only `tools/` module (own go.mod,
never tagged, outside the root test surface) -- the machine-checkable
rules of this file run as tests in `tools/conventions`, via `just
verify`. Production code never imports anything under tools/.

The domain layers:

- `pkg/<x>` -- vocabulary only: pure read-models, consts, named error
  variables. Imports infrastructure only. No constructors for
  read-models, no Config types, no fields without production readers.
- `pkg/<x>/controller` -- the only path to persistence: all public verbs, ALL
  input validation, `to*` adapters, schema asserts. Files: `<x>_config.go`,
  `controller_config.go`.
- `pkg/<x>/controller/datastore` -- all SQL; trusts inputs, no re-validation.
  Table-exact `*Data` structs live in `model.go`, never beside the query that
  returns them. An enum type travels with its const block.
- Import arrows point strictly downward.
- Named error variables are declared in the owning domain's `pkg/<x>`
  vocabulary (`errors.go`); an error value shared across different stacks
  lives in `pkg/common`. Whichever layer detects the condition raises it --
  admin for guards it composes, a datastore for facts its own query
  discovers.
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
  same-named private, then the next pair; deeper helper methods a private
  calls follow the pair that uses them, while free (non-receiver) funcs go in
  the file's bottom helper block (see File layout). Never all publics then
  all privates.
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
- A Config struct holds ONLY optional fields: every field is either filled
  by WithDefaults or meaningful at zero. A Validate error on a field
  WithDefaults never fills is a required value hiding in the config -- move
  it into the constructor's params.
- Config fields order domain-first, grouped by concern with blank lines,
  and end with the ambient tail: Logger, Retry, then any per-loop retry
  curves (SweepRetry, TickRetry). WithDefaults and Validate walk fields in
  declaration order; a default computed from other fields may trail its
  inputs instead.
- Param order is primary collaborator first, ambient last: the dep the struct
  is *about* leads, then its remaining deps, then `cfg`, and a bare
  `log logging.Logger` always trails (prefer `cfg.Logger` over a bare param).
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
- A field's absence is its zero value, and only where zero can never be real
  data -- mark it with a `// "" if unset` comment. A zero that could be real
  data means the design needs a named state or a widened domain (Batcher's
  `ShutdownGrace < 0`), never a nil pointer; a bool whose default would be
  true gets the inverted `Disable*` name so zero stays the default under
  `WithDefaults`. Absence of a whole entity is a nil struct return from its
  Get (the `(nil, nil)` comma-ok); pointers keep their one meaning --
  mutation/sharing -- and never encode optionality.
- A struct holding a mutex, atomic, or connection pool is pointer-only:
  copying it copies the lock, which is a data race, not a style slip.
- A value copy is not isolation: any slice, map, or pointer field inside it
  still aliases the original's backing memory, so mutating a copy can break
  the original's invariants.
- Accept interfaces only at real seams (`logging.Logger`; `Querier` stays
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
- Every SQL literal's first line is a comment naming its owner --
  `-- vulkan: <package>.<method>`. Constant text per query, so statement
  caching is unaffected; pg_stat_statements and the server log attribute
  load back to library verbs.

## Errors

Every error is five parts plus a recovery classification, carried by one
struct (diagnostic.Error) and rendered by one renderer per surface -- raise
sites never format anything. Renderer mechanics live in pkg/common/diagnostic/error.go
and the CLI errorHandler; the rules here are the choices the mechanism
cannot make.

The whole shape, one example:

    // pkg/topic/errors.go -- the declaration owns everything but the values
    var ErrTopicNotFound = diagnostic.NewError("VK0005", diagnostic.Permanent,
    	"topic not found",
    	"register it with MessageAdmin.RegisterTopic first")

    // raise site -- attach values, nothing else
    return topic.ErrTopicNotFound.With("topic", topicName, "version", version)

    // Error() one-liner (logs, wrapped chains) -- the code is the docs link
    topic not found: topic "orders", version 3 -- register it with
    MessageAdmin.RegisterTopic first [VK0005]

The CLI block, slog output, and --output json render these same parts as
fields; only the fix wording differs per surface (Go API in the library, a
vulkan command in the CLI).

### When declaring a new error condition

- A condition earns a declaration (and code) by any one of: a caller in
  another package branches on it with errors.Is; its recovery must differ
  from what IsTransientDatastoreError concludes on its own; it is a
  user-facing condition worth a docs page. Constructor/config validation,
  internal invariant guards, and unexported same-package control-flow
  signals stay plain errors on the templates below -- promote one to a
  declaration the moment it crosses the boundary.
- Declare a named Err* variable in the owning pkg/<x>/errors.go via
  diagnostic.NewError -- code, recovery, problem, and fix fixed at declaration.
- Code = "VK" + the next four-digit serial after the current max (same
  scheme as decision records). Never reuse or renumber; a deleted
  condition retires its number.
- Classify recovery by one question -- can an unchanged retry succeed?
  Transient = yes; Permanent = no. Retry machinery stops immediately on
  Permanent, so a wrong Transient burns a backoff curve on a lost cause.
- Land the docs page (…/errors/VK0005, headed by the verbatim problem
  text) in the same change -- readers and agents find it by pasting the
  message into search. Pages are hand-written under
  website/src/content/docs/errors/ (never generated); a change to a
  declaration's problem, recovery, or fix updates its page in the same
  change, and the page title stays the verbatim problem text.

### When writing the problem line

- One lowercase clause, fact only -- what is wrong and why. The fix is
  advice and lives in its own part, never blended into the fact.
- Use the template the condition kind already has: nil dep `<param> must
  not be nil` · required `<Field> is required` · empty `<param> must not
  be empty` · constraint `<Field> must be <constraint>` · absence
  `<noun> not found` · conflict `<noun> already <state>`.
- Tense follows recovery (test-enforced): Transient reads "could not
  <verb>"; Permanent reads "cannot" / "is" / "must".
- Never write: "failed", "invalid", "bad", "illegal", "unable", "unknown",
  "error", "please", "sorry", exclamation points, the raising function's
  name, or blame ("you passed"). "unrecognized <thing>: %q" replaces
  "unknown <thing>".
- When the outcome could be unclear, state what did or did not happen
  ("nothing was published").

### When writing the fix

- One imperative action naming the exact field, method, or command with
  the caller's real values interpolated; a CLI fix runs verbatim as
  pasted -- one that doesn't is a bug.
- A closed set names every legal value, so the caller fixes the input
  without opening docs; a near-miss gets offered ("a topic with a similar
  name exists: \"order\"").
- Leave the fix empty only when the code cannot know it -- never guess a
  cause or remedy.

### When raising and wrapping

- Attach values only through With, as named pairs -- identifiers quoted,
  durations and sizes with units. Never fmt.Errorf a part the struct owns.
- Branch with errors.Is against the Err* variable, never by matching
  message text -- wording stays free to improve everywhere at once.
- A wrapping layer adds only the fact it owns (`item %d: %w`); an error
  is returned or logged, never both.

### When writing a plain error

The errors that stay below the declaration boundary -- validation guards,
internal invariants, same-package control-flow signals.

- The problem-line templates, banned words, and tense rules above apply
  identically -- a plain error is the same fact minus the code, recovery,
  and registry. Before writing prose, check the 24 declared conditions:
  restating one is a bug, raise the Err* variable.
- A constraint guard ends with the violating value:
  `<name> must be <constraint>, got <value>` -- %d for ints, %v for
  durations (units come free), %q for strings. Absence guards
  (nil / required / empty) carry no value clause.
- `<name>` is the identifier as the caller knows it: the param name for
  constructor args, the exported field spelled exactly for config fields,
  the column or JSON key when validating stored data.
- `errors.New` for static text; `fmt.Errorf` only when a value is
  interpolated.
- A plain error may carry the same ` -- <fix>` clause, under the
  fix-writing rules above. A fix naming another package's method or a
  CLI command is the promotion tell -- the condition is user-facing, so
  declare it.
- Wrapping is the same rule as above -- only the owned fact, spelled as
  declared: `<Field>: %w` in config Validate chains, `item %d: %w` per
  element. Never restate the cause's content.

## Logging

Logs and errors are one message system with two mouths: an error speaks
when a caller receives a value; a log speaks when there is no caller.
They share the attr vocabulary, the problem-line grammar, and a
classification question.

### The seam

- Every log call goes through a config's `logging.Logger` and passes the
  caller's ctx -- `context.Background()` in a log call is a bug outside
  process-shutdown paths (there, `context.WithoutCancel(ctx)`).
- The default logger writes text lines to stderr, WARN and up. Logs never
  share stdout with program output.
- `logging.NewPipelineLogger` is the ONE wrapper: its config declares
  what the pipeline composes -- `Buffer` (WithLogBuffer boundaries),
  `Suppress` (repeat collapse), `Args` (bound attrs) -- and building
  over an existing pipeline merges instead of nesting.
- Identity is bound once: a long-lived component binds its attrs at
  construction -- `NewPipelineLogger` with `Args` -- and its call sites
  never repeat the bound keys.
- A long-lived instance (producer, consumer, system manager) declares
  `Buffer` + `Suppress` once at construction: repeats of one (level,
  message) Warn/Error line inside a one-minute window collapse to the
  first line, and the next emission carries the dropped total as
  `suppressed_count`. All the instance's workers share the window.

### Levels

Classify by one question -- who must act? -- the sibling of
Transient/Permanent (can an unchanged retry succeed?).

- Error: vulkan's own machinery stopped doing its job and no caller
  receives an error value -- a backoff curve exhausted, a worker
  suspended, a lock that could not be released. An operator must act.
- Warn: degraded but self-healing, or a durable data consequence -- a
  lease reclaimed from an expired worker, a message dead-lettered,
  stored options clamped. An operator should learn of it eventually.
- Info: lifecycle transitions and completed admin verbs only -- instance
  started/stopped, topic registered/destroyed, partition dropped, N rows
  swept. Info volume tracks state changes, never traffic.
- Debug: per-message and per-attempt narration -- claims, produces,
  batches, retries in progress. The domain working as designed
  (duplicate publish skipped, request superseded) is Debug no matter how
  dramatic it looks.

Steady state is silent: a tick that changed nothing logs nothing at any
level; a tick that changed rows logs one line with counts, never a line
per row.

An error is returned or logged, never both (the ## Errors rule): the
caller owns what it receives; a layer with no caller -- a goroutine top,
a tick loop -- is the one place logging a failure belongs.

### Messages

- The message is a static lowercase clause naming the event, constant
  across occurrences; every variable fact is an attr. A value
  interpolated into a message is a bug.
- Problem-line rules apply verbatim: the banned words, tense follows the
  fact (a self-healing failure reads "could not <verb>"; a completed
  transition reads past participle -- "topic registered", "lease
  reclaimed"), consequence or next action after ` -- `.
- Nothing branches or filters on message text -- not code, not labs.
  Labs assert on log events by level and attrs through a counting
  Logger, never by matching message substrings.

### Declared events

The mirror of the error-declaration boundary: a Warn or Error event
earns a declaration (and a code) when it is operator-actionable enough
for a docs page -- a durable data consequence, a reclaim, a backstop, a
stopped mechanism. Debug/Info narration never declares.

- Declare in the owning vocabulary package's logs.go via
  diagnostic.NewEvent(code, message, consequence) -- the codes share the
  errors' VK serial space, next four-digit serial after the current max
  across both registries.
- Call sites log the declaration's Message and attach `"code",
  Event.Code` as the first attr pair -- the message stays static, the
  code is the greppable pointer.
- Land the hand-written docs page (same /errors/ path) in the same
  change; `vulkan explain` lists events beside errors.
- The message follows the ### Messages grammar; the consequence clause
  is fixed at declaration, never at the call site.

### Attrs

- One key per concept, flat snake_case, spelled from this table; a new
  concept adds its row in the same change:

      error         the error value itself (never `err`, never
                    stringified first -- .Error() defeats
                    diagnostic.Error.LogValue)
      code          a declared log event's code (Event.Code)
      topic         topic name
      topic_id      topic id
      version       schema version
      group         consumer group name
      group_id      consumer group id
      system_id     system id
      owner         owner name (Owner.Name)
      owner_kind    owner kind (Owner.Kind())
      worker        worker name
      message_id    message id
      cron_job      cron job name
      cron_job_id   cron job id
      low, high     id range bounds
      attempt       retry position (attempts = the row's own column; a
                    cap spells its config field, max_retries)
      delay         backoff delay
      rate          worker poll rate
      vulkan_version  module version (common.BuildVersion) -- start lines
      <verb>_count  rows affected by the named action (swept_count,
                    reclaimed_count, dead_count)
      suppressed_count  repeats of the same Warn/Error line dropped
                    inside the suppression window

- Counts of affected rows end in `_count`; durations pass as
  time.Duration values (units render free); ids use their column's own
  name.

### The start line

A long-lived instance's "starting" line is its diagnosis snapshot: a
pasted log answers "what was your setup?" without a second question. It
carries the module version (common.BuildVersion), the instance identity,
and the resolved config facts an operator would ask for (poll rate,
timeouts, batch sizes) -- one line, attrs only. A config fact's key
spells its config field snake_cased (shutdown_timeout, batch_limit).
The paired "stopped" line carries the bound identity and nothing else.

### The failure record

- A Warn or Error event names enough domain state to reconstruct the
  picture cold -- the ids, the range, the attempt, the durations: the
  operands, not just the verdict.
- Operations carry a debug buffer: a boundary (logging.WithLogBuffer)
  opens a small per-operation ring; Debug/Info/Warn records inside it
  are held as well as forwarded, and the operation's first Error record
  drains the ring into its `preceding` group attr -- the failure line
  ships its own narration. Boundaries today: the produce call, the
  per-delivery dispatch, the worker tick; a new operation shape adds its
  boundary when built.

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