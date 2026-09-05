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

### Creating topics

Topics are created from your code, by `client.Topic(name).Register`. There is no
`vulkan topic register`: the CLI reads config and never writes it, so a
topic created from a shell would just be overwritten by the next call your
code makes. `vulkan system` and `vulkan schedule` work the same way — schedules
come from `client.Scheduler(name).Register`.

Names are dot-namespaced by domain and entity, `<domain>.<entity>[.<event>]`
(e.g. `orders.created`, `billing.invoice.paid`); topics are addressed by id
internally, so a name is safe to rename later.

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

### Read a message key (proposed)

Not shipped yet; this section is the spec. `topic key get` prints the
key's compaction head, the message that currently wins under it. The
CLI has no message type in scope, so the payload prints as the JSON the
row stores:

```console
$ vulkan topic key get orders.created order-42
✓ compaction head for "order-42" on "orders.created"

  MessageId        4187
  CreatedAt        2026-09-04 13:02
  RoutingKey
  CompactionRank   3
  Message
    {"order_id": "order-42", "status": "shipped", "total_cents": 1299}
```

A key nothing was produced under exits non-zero with VK0066. `--output
json` prints the message document with the payload inline under
`message`, not string-escaped.

`topic key messages` prints the key's retained history, newest first:

```console
$ vulkan topic key messages orders.created order-42 --limit 3
MESSAGE_ID   CREATED            RANK   MESSAGE
4187         2026-09-04 13:02   3      {"order_id": "order-42", "status": "shipped", ...}
4102         2026-09-04 11:47   2      {"order_id": "order-42", "status": "packed", ...}
3990         2026-09-04 09:15   1      {"order_id": "order-42", "status": "placed", ...}

3 messages
```

- `--limit` — newest N messages, default 20

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

- `-q` / `--quiet` — `list` prints names only; `get` prints nothing (the exit
  code is the answer).
- Exit codes: `0` success · `1` operation failed (not found, not empty, config
  mismatch, aborted) · `2` usage error.
