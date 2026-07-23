# vulkan

Admin CLI for [Vulkan](../../) topics — register, inspect, and destroy topics
against the control-plane Postgres.

## Install

```sh
go install github.com/agentstax/vulkan/cmd/vulkan@latest
```

## Connect

Every command needs a privileged Postgres URL, passed by flag or environment:

```sh
export VULKAN_ADMIN_DATABASE_URL="postgres://user:pass@host:5432/db"
# or per-command: --database-url "postgres://..."
```

This is deliberately **not** `DATABASE_URL` — that's your app's low-privilege
runtime role. The CLI runs DDL and `DROP`, so it wants admin credentials wired
in on purpose. The database must already have the Vulkan schema applied.

## Usage

### Register a topic

Idempotent — re-running with the same config is a no-op. Names are
dot-namespaced by domain and entity, `<domain>.<entity>[.<event>]` (e.g.
`orders.created`, `billing.invoice.paid`); topics are addressed by id
internally, so a name is safe to rename later.

```console
$ vulkan topic register orders.created --retention-ttl 720h
✓ registered topic "orders.created" (id=42)
```

A *different* config on an existing name is refused (changing config is
`alter`'s job, not `register`):

```console
$ vulkan topic register orders.created --retention-ttl 168h
error: topic "orders.created" already exists with a different configuration

  FIELD          EXISTING   REQUESTED
  RetentionTTL   720h0m0s   168h0m0s

register cannot change an existing topic's config -- that's alter's job.
```

Flags mirror the topic config — `--partition-size`, `--retention-ttl`,
`--idempotency-key-ttl`, `--janitor-poll-rate`, `--janitor-sweep-batch-size`,
`--allow-drop-past-committed`, `--disable-delivery-log` — and anything left
unset uses the library default. See `vulkan topic register --help` for each.

### List topics

```console
$ vulkan topic list
NAME             CREATED            UPDATED
billing.paid     2026-07-22 14:03   2026-07-22 14:03
orders.created   2026-07-20 09:11   2026-07-21 16:40

2 topics
```

`list` is a scannable overview; `get` shows a topic's full config.

### Get one topic

```console
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
```

A missing topic exits non-zero, so `get -q` doubles as an existence check:

```sh
if vulkan topic get -q orders.created; then echo "exists"; fi
```

### Destroy a topic

Prompts for the topic name before deleting anything:

```console
$ vulkan topic destroy orders.created
This will PERMANENTLY delete topic "orders.created" (id=42) and every message it holds.
This cannot be undone.

Type the topic name to confirm: orders.created
destroying "orders.created"... done
✓ topic "orders.created" destroyed
```

- `--force` — required to delete a topic that still holds messages
- `--yes` — skip the prompt (for CI). Does **not** imply `--force`.

```sh
vulkan topic destroy orders.created --force --yes
```

## Scripting

- `--json` — machine-readable output on `list` and `get`.
- `-q` / `--quiet` — `list` prints names only; `get` prints nothing (the exit
  code is the answer).
- Exit codes: `0` success · `1` operation failed (not found, not empty, config
  mismatch, aborted) · `2` usage error.
