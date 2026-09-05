---
status: accepted
date: 2026-09-04
phase: "pre-v1"
---

# 0646 — the topic handle carries the message type; a message key is a handle under it

Amends [0619] and [0625].

**Context.** `TopicHandle.CompactionHead[T]`, `AlertHandle.Get`, and
`MeasurementHandle.Get` all read "the head under a message key": Alert
and Measurement were key handles over built-in topics with the general
form missing. The type argument sat on every reading verb and every
Register, so one program spelled the same type at every call. Go 1.27
generic methods can return a generic type, so the type can move up.

**Decision.** `client.Topic[Message Versioned](name) *TopicHandle[Message]`,
the type argument required on every topic handle. Everything under it
inherits the type and takes none of its own: `Group(name).Register(ctx,
cfg, handler)` (was `client.Consumer`; `ConsumerHandle` is deleted),
`Producer().Register(ctx, cfg)` (was `client.Producer`),
`CompactionHeads(ctx, limit)`, and `Key(messageKey) *KeyHandle[Message]`
with `MessageKey()`, `CompactionHead(ctx)`, and `Messages(ctx, limit)`.
Admin verbs ignore the type, so a topic holding two schema versions is
two handles on the same rows. The key is the resource, not a `Compaction()` sub-handle:
the key is a message property with two readers [0612], and lease
facts land on the same handle later.

`CompactionHead` returns `compaction.ErrCompactionHeadNotFound`
(VK0066, Permanent, new `pkg/compaction/errors.go`); only `Get` verbs
are comma-ok. `AlertHandle.Get` and `MeasurementHandle.Get` become
one-liners over `Topic[Alert](alert.TopicName).Key(k)` and map the
error back to `(nil, nil)`.

`common.RawPayload []byte` serves callers with no message type in
scope, the CLI and admin-only scripts: `SchemaVersion()` returns 0 and
the `json.RawMessage` marshal pair keeps the stored bytes verbatim.
Producer, group, and scheduler Register refuse a version below 1 with
a plain error. The CLI gains `topic key get <topic> <key>` and `topic
key messages <topic> <key> --limit`. Schedules stay system-rooted on
`client.Scheduler(name)`: only Register knows a payload type, inferred
from the payload argument, and every other verb is keyed by name. The
handle's type parameter is `Message`; `common.Message[Payload]` keeps
its name.

**Consequences.** Amends [0619]'s clause "a struct is generic only when
it holds Message-typed state": a handle is generic to carry the type
downward, holding none; controllers and datastores stay plain structs
with `[Message]` on the method. Compile-time breaking on the client,
JSON unchanged. The ROADMAP item "Register returns what you run, the
client holds the assemblers" folds into this build.

Rejected: `Topic().Compaction()` as a mechanism sub-handle (the key
stays positional, `Messages` is not a compaction fact, lease state
would need a sibling); `vulkan.Of[T](handle)` (two handle types per
level); a topic-rooted schedule (a redundant topic on seven of eight
verbs and on every `vulkan schedule` command); an untyped `Topic(name)`
beside the typed one (Go cannot overload). Deferred to ROADMAP Later
beside the upcaster: a mismatched-version head read decodes silently.
