# `vulkan` admin CLI — design

Status: **`topic register/list/get/alter/rename/destroy` and `migrate` are
built and live-verified.** This records the command surface, per TODO.md's
"v1 admin surface" entry (`cmd/vulkan CLI (repo's first binary) wraps
pkg/admin thinly`).

**Implementation landed in passes** (DECIDED): `topic register/list/
get/destroy` first, since `pkg/admin` already backed all four from day one.
`migrate` landed next (2026-07-24), once `admin.MigrateSystem`,
`MigrateTopics`, and `MigrateTopic` all existed. `alter` and `rename` landed
last (2026-07-24), once `admin.AlterTopic`/`RenameTopic` existed to back
them -- `alter` was blocked until the field-mutability question was settled
(PartitionSize is immutable, so it's absent from the surface; name change is
its own `rename` verb). See "Implementation scope" below.

The CLI is a thin wrapper over `pkg/admin`'s methods (`RegisterTopic`,
`GetTopic`, `ListTopics`, `AlterTopic`, `RenameTopic`, `DestroyTopic`,
`RegisterSystem`, `MigrateSystem`, `MigrateTopics`, `MigrateTopic`) plus two
direct `pkg/migrate` reads (`migrate.Version`, `migrate.IsLocked`) for
`status`/`versions` and the lock pre-flight -- see "`vulkan migrate`" below
for why those two reach past `pkg/admin`. Internals of
`pkg/admin`/`pkg/topic`/`pkg/migrate` are otherwise out of scope here -- this
is the user-facing surface only.

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

**Lip Gloss** (`charmbracelet/lipgloss`, v2) for the colored ✓/✗/⚠ status
glyphs, gated on `colorEnabled()` (a real TTY, no `NO_COLOR`/`TERM=dumb`) so
piped/CI output stays plain text. Tables (`topic list`, `topic get`,
`migrate versions`/`status`) are stdlib `text/tabwriter` -- Lip Gloss's own
table component isn't in use.

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

**Output**: human-readable table by default. `list`/`get` also take
`-q`/`--quiet` (names-only / no detail block) for shell composition
(`docker`/`kubectl -o name` convention). (`--json` was removed for now --
no consumer yet; re-add when something needs machine-readable output.)

**Exit codes**: `0` success · `1` operation failed (not found, not empty,
config mismatch, destroy aborted) · `2` usage error (bad flags, refusing to
run non-interactively).

**Errors**: always human text on stderr, with an `error: ` prefix.

**Unregistered system**: any `topic` subcommand run before the system schema is
registered should surface the teaching error `pkg/admin` already returns
(`RegisterTopic` wraps `migrate.ErrNotRegistered`) rather than a raw `42P01
undefined_table`:

```
error: system schema not registered -- register the system first
```

## Implementation scope

What's buildable against `pkg/admin`/`pkg/topic`/`pkg/migrate`/`pkg/datastore`
as they stand today, with zero changes to library code:

- **`register`/`list`/`get`/`destroy`** — `RegisterTopic`, `ListTopics`,
  `GetTopic`, `DestroyTopic`+`DestroyOptions{Force}` all exist;
  `ErrTopicConfigMismatch`/`ErrTopicNotFound`/`ErrTopicNotEmpty`/
  `ErrDestroyDisabled` are all already `errors.Is`-distinguishable for the
  CLI's per-case messages. The CLI sets `AllowDestroy: true` via the
  existing `MessageAdminConfig` field -- no library change needed for that
  either.
- **`migrate`** — built (2026-07-24). `admin.RegisterSystem`/`MigrateSystem`/
  `MigrateTopics`/`MigrateTopic` back `init`/`system`/`topics`/`topic`;
  `status`/`versions` read `pkg/migrate` directly (see below).
- **`alter`/`rename`** — built (2026-07-24). `admin.AlterTopic` (sparse
  `topic.AlterConfig` patch) backs `alter`; `admin.RenameTopic`
  (+ `ErrTopicNameTaken`) backs `rename`. See "`vulkan topic alter`" and
  "`vulkan topic rename`" below.

**Connection wiring caveat** (found while scoping `topic`, still true):
`datastore.NewPostgresDatastore` takes a `PostgresConnectionConfig{User,
Pass, Host, Port, Database, MaxConns}` struct, not a URL -- there's no
`postgres://...` constructor today. The CLI honors
`--database-url`/`VULKAN_ADMIN_DATABASE_URL` without any `pkg/datastore`
change by parsing the URL itself and populating that struct
(`internal/cli/conn.go`). The gap: `PostgresConnectionConfig` has no field
for `sslmode` or any other DSN query parameter, so anything beyond
user/pass/host/port/db in the URL is dropped with a warning today. Not
blocking against a plain local/CI Postgres, but will need a `pkg/datastore`
change (a field, not a redesign) the moment someone points `--database-url`
at something requiring `sslmode=require` et al.

---

## `vulkan migrate`

> Data-model note: the backing is a single `schema_log` table keyed by
> `(entity_type, entity_id)` -- `('system', 0)` and `('topic', topic_id)`.
> Current version = latest-by-`id` row where `status = 'success'`.

**Two independent scopes, not one.** System (the shared control-plane
tables) and topic (per-topic table families) version SEPARATELY -- each has
its own registry, its own ceiling, its own current version per entity. A
topic's version only moves when a topic-scope step actually runs for it;
it does NOT advance just because a system-scope step ran. This resolved the
"does every topic's stamp advance on every step" open question below in the
negative -- the two scopes never touch each other's version.

```
vulkan migrate init                          # RegisterSystem -- create the control-plane schema at v1 (idempotent)
vulkan migrate versions                      # versions this binary knows, per scope -- no DB read
vulkan migrate status                        # current (DB) vs available (binary), system + every topic
vulkan migrate system  up|down  --to N       # MigrateSystem
vulkan migrate topics  up|down  --to N       # MigrateTopics -- every registered topic
vulkan migrate topic   up|down <name> --to N # MigrateTopic -- one topic
```

`init` isn't a migrate STEP -- it's the same `RegisterSystem` bootstrap
`admin.RegisterSystem` backs elsewhere, surfaced here because "stand up the
schema" belongs conceptually with the rest of schema management, not
scattered as its own top-level verb. `topic register` still gates on it
having run (see "Unregistered system" above); this is what unblocks that gate.

### `init`

Idempotent, matching `RegisterSystem`:

```
$ vulkan migrate init
✓ system schema initialized (version 1)
```

### `versions`

Lists every schema version compiled into *this binary*, per scope -- the
step registry is the source of truth, nothing is read from the database.
Steps carry no description in the registry (a house-style choice -- the
registry file itself, and release notes, are where that lives), so `versions`
prints numbers, not prose:

```
$ vulkan migrate versions
system schema versions (this binary):
  1  baseline
  2
  3

topic schema versions (this binary):
  1  baseline
  (no versioned steps compiled in yet)
```

### `system` / `topics` / `topic` `up` / `down`

`--to` is mandatory on every leaf -- explicit target, no implicit "to
latest" on `up`, no implicit "one step back" on `down`. The CLI enforces
direction itself (the runner would happily go either way from a bare
target): `up` refuses a target that would roll the schema back, `down`
refuses one that would move it forward:

```
$ vulkan migrate system up
error: --to is required (e.g. --to 3) -- run `vulkan migrate versions` to see what's available

$ vulkan migrate system down
error: --to is required for system down -- downgrades name an explicit target, there's no implicit "down one step"

$ vulkan migrate system down --to 5
error: --to 5 is out of range [1, 3] for this binary -- run `vulkan migrate versions` to see what's available

$ vulkan migrate system down --to 3
error: system is at version 2; --to 3 is not a downgrade -- use `up` to move forward

$ vulkan migrate system up --to 2
✓ system migrated up to version 2

$ vulkan migrate system up --to 2     # run again, already there
✓ system already at version 2, nothing to do

$ vulkan migrate topics up --to 2
✓ migrated 3 topics up to version 2

$ vulkan migrate topics up --to 2     # no topics registered yet
no topics registered

$ vulkan migrate topic up orders.created --to 2
✓ topic "orders.created" migrated up to version 2

$ vulkan migrate topic up ghost.topic --to 2
error: topic "ghost.topic" not found

$ vulkan migrate system up --to 3
error: another migration is already in progress (advisory lock held) -- wait for it to finish, or confirm no other migrate process is actually running before retrying
```

The lock-contention error is a fast pre-flight (`migrate.IsLocked` against
`pg_locks`, checked right before the real call), not a guarantee -- another
process can still win a race in the gap between the check and the actual
lock acquisition. That residual case falls back to today's behavior
(the call blocks until the lock frees), same as before this check existed;
deliberately not closed with a client-side timeout, since the only way to
bound just the "waiting for the lock" phase without also risking killing a
legitimately long-running migration step would require splitting lock
acquisition out of the library's `Runner.RunOnce`/`RunAll` as its own call --
more surface than an edge case this rare justifies.

### `status`

Compares two numbers per schema (system, and every registered topic):

- **current** — read from the DB via `migrate.Version` (schema_log latest-by-id
  success row).
- **available** — not a DB read. The same binary-compiled ceiling `versions`
  prints, per scope. An older CLI against a newer DB can say "I only know up
  to 2, this DB is at 3" without that being an error (`current > available`
  is not "behind" -- only `current < available` is actionable).

```
$ vulkan migrate status
latest available: system 3, topic 1

SCHEMA            CURRENT   AVAILABLE
system            2         3
orders.created    1         1
billing.paid      1         1

system behind (2 < 3) -- run `vulkan migrate system up --to 3`

$ vulkan migrate status     # before init
system schema not initialized -- run `vulkan migrate init`
```

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

## `vulkan topic alter <name>`

Backed by `admin.AlterTopic`, which takes a sparse `topic.AlterConfig` --
pointer fields where nil means "leave unchanged." The CLI mirrors that
exactly: each flag maps 1:1 to a field, and only the flags actually passed
(`cmd.Flags().Changed`) become non-nil. It's a **patch, not a replace** --
unmentioned fields keep their stored value, which is why the operator doesn't
have to restate the whole config to change one field.

Two fields are deliberately absent from the surface:

- **`--partition-size`** — immutable. Every existing partition's bounds were
  computed from it, so it can't change mid-life (see TODO.md's "dynamic
  partition bounds" entry for the follow-up that would unlock it). `pkg/topic`
  enforces this by construction: `AlterConfig` has no `PartitionSize` field.
- **the name** — changing it is `topic rename` (below), not a config field.

Bools are genuine tri-state: `--disable-delivery-log=false` counts as
*passed*, so it sets the field false, distinct from omitting the flag (leave
as-is). Success prints an OLD → NEW table over just the fields that changed:

```
$ vulkan topic alter orders.created --retention-ttl 1440h --janitor-sweep-batch-size 5000
✓ altered topic "orders.created" (id=42)

  FIELD                   OLD        NEW
  RetentionTTL            720h0m0s   1440h0m0s
  JanitorSweepBatchSize   1000       5000
```

Passing no field flags is a usage error (exit 2), not a silent no-op -- an
alter must change something. Out-of-range values are rejected up front by
`AlterConfig.Validate` before any DB call, same exit 2:

```
$ vulkan topic alter orders.created
error: no fields set -- an alter must change at least one field

$ vulkan topic alter orders.created --janitor-sweep-batch-size 0
error: JanitorSweepBatchSize must be > 0, got 0

$ vulkan topic alter ghost.topic --retention-ttl 1h
error: topic "ghost.topic" not found
```

Two consequences carried in the command's `--help` (from `AlterTopic`'s own
contract): a running producer/consumer snapshots config at *its* Register, so
an alter takes effect on its next restart, not live; and a `RegisterTopic`
call still passing the pre-alter config will fail `ErrTopicConfigMismatch` --
deliberate, so a declarative register can't silently paper over an operator's
change.

---

## `vulkan topic rename <name> <new-name>`

Backed by `admin.RenameTopic`. Everything but the name -- id, config, stored
messages -- is untouched, since every table is addressed by id internally, so
this is a single metadata-only row update:

```
$ vulkan topic rename orders.created orders.v2
✓ renamed topic "orders.created" -> "orders.v2" (id=42)
```

Error cases map the two sentinels plus the same-name guard:

```
$ vulkan topic rename ghost.topic whatever
error: topic "ghost.topic" not found

$ vulkan topic rename orders.v2 billing.paid       # target name already taken
error: topic "billing.paid" already exists -- pick a name that's free, or destroy it first

$ vulkan topic rename orders.v2 orders.v2
error: new name matches the current name -- nothing to rename

$ vulkan topic rename orders.v2
error: rename requires a topic name and a new name
usage: vulkan topic rename <name> <new-name>
```

The hazard worth knowing (in `--help` too): the **old name is free the moment
this returns**. Running producers/consumers keep working -- they resolved the
id at their Register -- but anything still *configured* with the old name
fails its next restart's Register, or worse, silently attaches to a new topic
later registered under the freed name. Update those configs before reusing
the old name.

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

- Whether to bring back a machine-readable output mode (`--json` was removed
  in the first pass for want of a consumer).

**Resolved**: `alter`'s CLI surface (was: undesigned, since some config like
`PartitionSize` is baked into per-topic table shape and may not be a simple
field update) -- built 2026-07-24. `PartitionSize` is immutable and simply
absent from the surface; the name is `rename`'s job, not a config field;
every remaining field is a plain sparse-patch update. See "`vulkan topic
alter`"/"`vulkan topic rename`" above.

**Resolved**: `migrate status`'s per-topic-stamp question (was: does every
topic's version advance on every step, even system-only ones?) -- no. System
and topic are fully independent version counters; a topic's version only
moves when a topic-scope step runs for that specific topic. See "`vulkan
migrate`" above.
