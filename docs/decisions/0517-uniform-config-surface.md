---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0517 — Uniform config get/set/unset surface; alter dies in the CLI; Go Alter* verbs take tri-state Update[T]

**Context.** [0515] shipped `vulkan group alter --clear` for the override
layer, leaving the CLI with two config vocabularies: typed set flags vs
stringly clear keys, and per-resource `alter` beside the new surface.
Research: Postgres pairs SET/RESET on one key vocabulary; Kafka migrated
topic config OUT of `kafka-topics --alter` into the unified `kafka-configs`
surface and spent years deprecating the split; Helm `--set a.b=c` set the
dotted-path precedent. The repo has two alter semantics: layered
(group/worker metadata: unset removes a real override) and absolute
(topic/cron columns: no layer underneath).

**Decision.**
- Every resource speaks `vulkan <resource> config get|set|unset`; `alter`
  exists nowhere in the CLI. Lifecycle verbs stay verbs (register, destroy,
  rename, migrate, suspend, run, scale). The system alter stub is deleted
  outright rather than kept for future knobs (amends [0516]).
- One rule: set writes the value; unset returns the key to its default; a
  key with no default (cron schedule/data/metadata) is set-only. Layered
  resources unset by removing the override; absolute resources unset by
  writing the `WithDefaults` value, resolved in admin. Register-time-only
  fields (partition_size) are not config keys.
- One key vocabulary: the names `config get` prints (metadata JSON tags /
  column names) are what set/unset accept. Each resource's CLI file holds
  one key table (parse, format, help line) — the single home for key
  knowledge; unknown keys error listing the known ones.
- Dotted paths patch JSON-doc keys via read-patch-write. Known-shape keys
  (message.*) are typed by the table and forbid whole-doc set (raw JSON is
  ns-encoded). Arbitrary payloads (cron data.*) infer JSON-literal-else-
  string, allow whole-doc set, and `unset data.<path>` deletes the field —
  absence is an arbitrary field's default.
- The Go API keeps one Alter* verb per resource ([0515]'s AlterGroup shape
  stands; amends only its CLI wording). Defaulted config fields become
  `common.Update[T]`: zero value = leave unchanged, `common.Set(v)`,
  `common.Unset[T]()` = back to default. Set-only fields stay plain
  pointers — a field advertises exactly the states it has. No key strings
  in Go signatures.

**Consequences.** AlterTopicConfig/AlterCronJobConfig/AlterGroupConfig
reshape onto Update[T]; `alter.go`/`cron_alter.go`/`system_alter.go` and
bare `group get`/`group alter` are deleted; `topic get` drops the alterable
columns; the worker CLI ([0515] poweruser tier) becomes
`worker config` + scale/suspend/resume. Rejected: split Set*/Unset* admin
verb pairs (typed-set/stringly-unset asymmetry), a `default` value sentinel
(reserves the word in every future string key), per-field --clear-* flags
(gcloud's flag sprawl).
