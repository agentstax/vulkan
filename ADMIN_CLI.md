# `vulkan` admin CLI — design

Status: **design only, not implemented.** `cmd/vulkan` doesn't exist yet. This
records the command surface agreed on before writing any code, per TODO.md's
"v1 admin surface" entry (`cmd/vulkan CLI (repo's first binary) wraps
pkg/admin thinly`).

**Implementation lands in two passes** (DECIDED): `topic register/list/
get/destroy` first, since `pkg/admin` already backs all four today.
`migrate` and `alter` are deliberately excluded from that first pass — not
stubbed — because neither has a backing implementation yet (`admin.Migrate`
doesn't exist, no `schema_version` table exists; `admin.Alter` doesn't
exist, TODO.md itself calls it "currently an impossible operation"). Both
are still committed to landing **before v1 ships**, as their own separate
implementation passes once `pkg/admin` grows the methods to back them --
just not in the same pass as the first four commands. See "Implementation
scope" below for the full "buildable today vs not" breakdown.

The CLI is a thin wrapper over `pkg/admin`'s methods (`RegisterTopic`,
`GetTopic`, `ListTopics`, `DestroyTopic`) plus `Migrate` (shape decided in
TODO.md's "migration scripts -> code" entry, not built yet). Internals of
`pkg/admin`/`pkg/topic` are out of scope here — this is the user-facing
surface only.

## Library stack

**Cobra** (`spf13/cobra`) for the command tree — subcommands, flags, help
generation, shell completions. Same foundation as `kubectl`/`docker`/`gh`.

**Fang** (`charmbracelet/fang`) wraps `cobra.Execute()` for styled
`--help`/usage/error output, automatic `--version`, man pages, completions,
and terminal color-profile detection (downsamples gracefully from
TrueColor to 16-color, adapts to light/dark backgrounds).

**Huh** (`charmbracelet/huh`) for the one interactive moment in the whole
CLI: `destroy`'s type-the-name-to-confirm prompt. Gives that prompt an
accessible (screen-reader) mode for free.

**Lip Gloss** (`charmbracelet/lipgloss`, v2) for tables (`topic list`,
`migrate status`) and the colored ✓/✗/⚠ status glyphs.

**Packaging**: `cmd/vulkan/` is a nested Go module with its own `go.mod` --
none of the above land in the root library module's dependency graph.
Decision and mechanics recorded in TODO.md's "v1 admin surface" entry, not
duplicated here.

### CI-safety

Two different risk profiles, not one:

- Lip Gloss/Fang (styling and output) sit on `termenv`, which detects
  non-TTY output and strips ANSI/color automatically — this is standard
  behavior, not something to opt into, and it's what every non-`destroy`
  command exercises in a pipeline (plain aligned text, no escape codes).
- Huh (and Bubble Tea generally) assumes a TTY and does *not* auto-degrade
  when piped — a known open issue (huh#101). This only matters for
  `destroy`, and `destroy` already requires a TTY check before it ever
  calls into Huh: non-interactive stdin without `--yes` hits the refusal
  error path (see below) and never invokes the confirm prompt at all. So
  the one risky component in the stack never actually runs in CI as
  designed.

Belt-and-suspenders: also check `CI`/`TERM=dumb` env vars alongside the
`isatty` check, since some CI log viewers present a pty-like stream that
can fool a bare isatty check. Smoke-test real output on whichever CI
providers this actually runs on before trusting it.

## Global conventions

**Connection**: `--database-url` flag, falling back to env
`VULKAN_ADMIN_DATABASE_URL`.

Deliberately not plain `DATABASE_URL`. The reason `pkg/admin` exists at all
is a privilege split — the CLI's connection is the privileged one (DDL,
DROP), while an app's `DATABASE_URL` is meant to be the low-privilege
runtime role. Reusing that var name risks a CI script's ambient
`DATABASE_URL` (meant for the app) getting aimed at a destructive admin
command. A distinctly named var forces the operator to wire in admin
credentials on purpose.

**Output**: human-readable table by default. `--json` for scripting.
`list`/`get` also take `-q`/`--quiet` (names-only / no detail block) for
shell composition (`docker`/`kubectl -o name` convention).

**Exit codes**: `0` success · `1` operation failed (not found, not empty,
config mismatch, destroy aborted) · `2` usage error (bad flags, refusing to
run non-interactively).

**Errors**: always human text on stderr, `error: ` prefix, even under
`--json` — only the success payload becomes JSON, so error handling never
has to branch on flags.

**Unmigrated DB**: any `topic` subcommand run before `vulkan migrate` has
ever run should translate Postgres's raw `42P01 undefined_table` into:

```
error: schema not initialized -- run `vulkan migrate` first
```

## Implementation scope (first pass)

What's buildable against `pkg/admin`/`pkg/topic`/`pkg/datastore` as they
stand today, with zero changes to library code:

- **`register`/`list`/`get`/`destroy`** — fully buildable. `RegisterTopic`,
  `ListTopics`, `GetTopic`, `DestroyTopic`+`DestroyOptions{Force}` all
  exist; `ErrTopicConfigMismatch`/`ErrTopicNotFound`/`ErrTopicNotEmpty`/
  `ErrDestroyDisabled` are all already `errors.Is`-distinguishable for the
  CLI's per-case messages. The CLI sets `AllowDestroy: true` via the
  existing `MessageAdminConfig` field -- no library change needed for that
  either.
- **`migrate` (all four subcommands) and `alter`** — not buildable, see
  above. Deferred, not stubbed.

**Connection wiring caveat** (found while scoping, worth keeping in mind
when this gets built): `datastore.NewPostgresDatastore` takes a
`PostgresConnectionConfig{User, Pass, Host, Port, Database, MaxConns}`
struct, not a URL -- there's no `postgres://...` constructor today. The CLI
can still honor `--database-url`/`VULKAN_ADMIN_DATABASE_URL` without any
`pkg/datastore` change by parsing the URL itself and populating that
struct. The gap: `PostgresConnectionConfig` has no field for `sslmode` or
any other DSN query parameter, so anything beyond user/pass/host/port/db in
the URL is silently dropped today. Not blocking for a first pass against a
plain local/CI Postgres, but will need a `pkg/datastore` change (a field,
not a redesign) the moment someone points `--database-url` at something
requiring `sslmode=require` et al.

---

## `vulkan migrate`

```
vulkan migrate versions
vulkan migrate up --to N       # --to required -- explicit, no implicit "to latest"
vulkan migrate down --to N     # --to required -- no implicit "one step back"
vulkan migrate status
```

### `versions`

Lists every schema version compiled into *this binary* — the step list
itself is the source of truth, not anything read from the database:

```
$ vulkan migrate versions
VERSION   DESCRIPTION
1         baseline: topic, cursor, per-topic tables
2         add delivery_log audit trail
3         add compaction latest_key index
4         add schema_version per-topic stamp

latest available: 4
```

### `up` / `down`

```
$ vulkan migrate up
error: --to is required (e.g. --to 4) -- run `vulkan migrate versions` to see what's available

$ vulkan migrate down
error: --to is required for migrate down (e.g. --to 3) -- downgrades name an explicit target, there's no implicit "down one step"

$ vulkan migrate down --to 5
error: schema is already at version 4, nothing to do (requested --to 5 is not a downgrade)

$ vulkan migrate up --to 4
error: another migration is already in progress (advisory lock held) -- wait for it to finish, or confirm no other migrate process is actually running before retrying
```

### `status`

Compares two numbers:

- **current** — read from the DB: the shared `schema_version` row, plus each
  topic's own `topic.schema_version` stamp (that per-topic stamp exists so a
  crash mid-migration-loop is resumable/visible).
- **latest available** — not a DB read. The same binary-compiled ceiling
  `versions` prints. An older CLI against a newer DB can say "I only know up
  to 3, this DB is at 4" without that being an error.

```
$ vulkan migrate status
latest available: 4 (vulkan migrate versions)

SCHEMA              CURRENT
shared              3
orders.created       3
billing.paid          4

shared, orders.created behind latest (3 < 4) -- run `vulkan migrate up --to 4`
```

Design assumption this leans on (worth confirming when `Migrate` is actually
built, not just a CLI-side wish): every topic's stamp must advance on
*every* step, even one that only touches shared tables and has nothing to
do for that topic. Otherwise "current" needs a second axis ("current *for
steps that applied to you*"), which is a worse status display.

---

## `vulkan topic register <name>`

Flags map 1:1 to `topic.Config`, kebab-case, **left unset by default** (not
hardcoded) so `WithDefaults()` stays the single source of truth — `--help`
annotates `(library default)` rather than duplicating the number:

```
--partition-size int
--retention-ttl duration
--allow-drop-past-committed
--idempotency-key-ttl duration
--disable-delivery-log
--janitor-poll-rate duration
--janitor-sweep-batch-size int
```

`RegisterTopic` is idempotent, so three outcomes:

```
$ vulkan topic register orders.created --retention-ttl 720h
✓ registered topic "orders.created" (id=42)

$ vulkan topic register orders.created --retention-ttl 720h     # run again, unchanged
✓ topic "orders.created" already registered (id=42) -- no changes

$ vulkan topic register orders.created --retention-ttl 168h     # different config
error: topic "orders.created" already exists with a different configuration

  FIELD           EXISTING     REQUESTED
  RetentionTTL    720h0m0s     168h0m0s

register cannot change an existing topic's config -- that's alter's job.
```

(The diff table is CLI-side: refetch via `GetTopic` and compare against the
config it just tried to send, rather than parsing the wrapped
`ErrTopicConfigMismatch` error string.)

```
error: register requires a topic name
usage: vulkan topic register <name> [flags]

error: invalid config: RetentionTTL must be >= 0, got -1h0m0s
```

---

## `vulkan topic list`

```
$ vulkan topic list
NAME             CREATED            UPDATED
billing.paid     2026-07-22 14:03   2026-07-22 14:03
orders.created   2026-07-20 09:11   2026-07-21 16:40

2 topics

$ vulkan topic list -q
billing.paid
orders.created

$ vulkan topic list
no topics registered
```

---

## `vulkan topic get <name>`

Maps straight to `GetTopic`. Full detail on hit, quiet not-found on miss,
exit code doubling as the boolean result:

```
$ vulkan topic get orders.created
✓ topic "orders.created" exists (id=42)

  CreatedAt                2026-07-20 09:11
  UpdatedAt                2026-07-21 16:40
  PartitionSize            1,000,000
  RetentionTTL             720h0m0s (30d)
  AllowDropPastCommitted   false
  IdempotencyKeyTTL        1h0m0s
  DisableDeliveryLog       false
  JanitorPollRate          5s
  JanitorSweepBatchSize    1000

$ vulkan topic get ghost.topic; echo $?
✗ topic "ghost.topic" does not exist
1
```

`if vulkan topic get -q X; then …` composes naturally in scripts.

---

## `vulkan topic alter`

No design exists in `pkg/admin` yet — TODO.md itself calls this "currently
an impossible operation." Not designing a CLI surface for a backend that
doesn't exist yet.

**Resolved**: omitted entirely from the CLI's first implementation pass,
not stubbed (see "Implementation scope" above) — clig.dev's own guidance is
against exposing a subcommand guaranteed to fail. Lands as its own
implementation pass once `Alter` gets its design pass in `pkg/admin`, and
is committed to landing before v1 ships either way.

---

## `vulkan topic destroy <name>`

Highest-stakes command, most design weight.

```
--force      # DestroyOptions{Force: true} -- required to delete a non-empty topic
--yes / -y   # skip interactive confirmation (CI)
```

`--yes` and `--force` are **independent gates** — CI skipping the prompt
doesn't imply consent to blow away undelivered messages; that still needs
`--force` said explicitly.

Order of checks, so a doomed call never wastes a confirmation prompt:

1. Look up the topic (`GetTopic`). Not found → error immediately, no prompt.
2. Not `--force` and topic not empty → error immediately, no prompt (the
   call would fail anyway).
3. Otherwise → confirm (unless `--yes`), then destroy.

```
$ vulkan topic destroy ghost.topic
error: topic "ghost.topic" not found

$ vulkan topic destroy orders.created
error: topic "orders.created" still holds messages -- pass --force to destroy anyway (this is unrecoverable data loss, not just a schema drop)

$ vulkan topic destroy orders.created
This will PERMANENTLY delete topic "orders.created" (id=42) and every message it holds.
This cannot be undone.

Type the topic name to confirm: orders.created
destroying "orders.created"... done
✓ topic "orders.created" destroyed

$ vulkan topic destroy orders.created --force
⚠ topic "orders.created" still holds messages -- --force will delete them along with the topic.
This will PERMANENTLY delete topic "orders.created" (id=42) and every message it holds.
This cannot be undone.

Type the topic name to confirm:

$ vulkan topic destroy orders.created --yes
destroying "orders.created"... done
✓ topic "orders.created" destroyed
```

Mistyped confirmation aborts immediately — no retry loop, so a script
piping the wrong thing doesn't hang or get three guesses at a destructive
op:

```
Type the topic name to confirm: orderz.created
aborted: input did not match topic name
```

Non-interactive stdin (piped/CI) without `--yes` refuses rather than
hanging on a read that will never resolve:

```
error: refusing to destroy "orders.created" without confirmation -- pass --yes in non-interactive contexts (e.g. CI)
```

`ErrDestroyDisabled` never surfaces here — the CLI sets `AllowDestroy`
internally (per TODO.md's decided shape), so that gate only matters to
library embedders, not this tool.

---

## Open questions

- Whether `migrate status`'s "every topic stamp advances on every step"
  assumption holds once `Migrate` is actually implemented.
- Whether `--json` should be available on `migrate status`/`versions` too
  (probably yes, not yet spec'd here).
