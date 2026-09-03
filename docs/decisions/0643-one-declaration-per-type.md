---
status: accepted
date: 2026-09-03
phase: "pre-v1"
---

# 0643 — One declaration per type, vulkan as the client plus aliases

**Context.** [0625] said "every user-spelled type lives in `vulkan`" and the
ROADMAP carried the follow-through: move the declarations in. Building it is
impossible in Go: a package that declares `TopicConfig` and is imported by
the topic controller cannot also import the assemblers that read it -- the
import graph forbids one package both declaring the types and assembling
the machinery. Alias chains ran four hops (vulkan -> producer -> controller
-> datastore), the hand-kept alias list was missing a dozen names, errors
and events were never re-exported, and seven codes sat below a root under
unexported names. River's facade-plus-types shape, not Temporal's
alias-into-internal, is the shape.

**Decision.** Four laws. (1) One declaration: every exported type, const,
named error, and declared event is declared once; the only `type X = pkg.X`
lines in the repo are pkg/vulkan/alias.go. (2) Placement by lowest reader,
with a floor: machinery (controller, datastore, batcher, worker) declares
nothing a user spells except its own Config and `*Row` structs and its
controller / datastore / instance / provisioner types, so a user-spelled
name lives in exactly one of `common` (two domains read it), a root (that
domain's machinery reads it), or an assembler (only its own verbs read it).
Codes are the same law with no assembler case: every `NewDiagnosticError` /
`NewDiagnosticEvent` / `NewDiagnosticMetric` initializes an exported var in a
root's errors.go, events.go, or metrics.go, or in common. (3) Roots are
named for what they are about: thing domains take the resource (topic,
system, worker, alert, metrics), activity domains the verb (schedule,
migrate, compaction, consume, produce), an activity's assembler its agent
noun (scheduler, consumer, producer); a root's own controller is
`<Root>Controller`. A root type whose bare name is a generic noun takes the
root's noun as prefix at the declaration (MetricKind, AlertStatus,
DiagnosticError; consts follow), so an alias stays same-named. (4) vulkan is
the client plus aliases: Client, ClientConfig, the pool, four handles, two
instance wrappers, two interfaces; everything else an alias or var into the
declaring package, the set computed by a go/types closure test; the client
holds assemblers only.

Amends [0625]: "every user-spelled type lives in `vulkan`" becomes "aliased
in `vulkan` from its declaring package"; the rest stands. Amends [0555]: the
domain kind gains the naming rule, the one-declaration law, and the floor;
`consumergroup` is `consume`, `produce` is a root, assemblers declare no codes.

**Consequences.** consumergroup -> consume, producer machinery -> produce,
`Versioned` -> common, `Tx`/`InTransaction` -> datastore, TopicConfig /
ScheduleConfig / ScheduleSpec / SystemConfig / alert JobConfigs to their
roots; ConsumerConfig gains Bindings; admin gains
GetGroup / ListGroups / ListGroupWorkers / GetCompactionHead / ListKeyMessages
so a handle verb is one call; vulkan's adapter.go and config twins are
deleted. Six tools/conventions tests enforce the laws: alias placement,
vulkan's imports, the closure comparing targets, the machinery floor, codes
at roots (doubling as registry completeness), SQL owner segments.
Rejected: an `internal/` tree (not now); a pkg/message root; dissolving
admin into vulkan; dropping type prefixes (a separate pass); a hand-kept
alias list.
