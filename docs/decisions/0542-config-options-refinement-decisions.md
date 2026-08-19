---
status: accepted
date: 2026-08-19
phase: 14b
---

# Config & options refinement: the three shape decisions

## Context

The 14b config pass opened with a survey: across all 48 Config structs the
required/optional split already held -- every field either WithDefaults-
filled or zero-meaningful -- except PostgresConnectionConfig, whose
User/Host/Database were required fields hiding in a config. The open
decisions were the OptionalConfig rename, the ProduceOptions compaction
shape, and Consumer.Register's five params.

## Decision

- Config keeps its name; the rule is codified in CONVENTIONS.md instead:
  a Config struct holds ONLY optional fields, and a Validate error on a
  field WithDefaults never fills marks a required value to move into the
  constructor's params. PostgresConnectionConfig fixed accordingly:
  NewPostgresDatastore(ctx, user, host, database, cfg); the config keeps
  Pass/Port/MaxConns/ConnectTimeout/TLSConfig.
- Compaction nests (user-picked over flat + Validate): ProduceOptions
  drops CompactionKey/CompactionRank for one Compaction *CompactionOptions
  {Key, Rank}, built with NewCompactionOptions(key, rank) -- rank 0 means
  arrival order. Rank-without-key is now unrepresentable; nil means not
  compacted (the MessageOptions nilable precedent, [0536]). The
  defer-requires-key rule survives as defer-requires-Compaction. Table-
  exact row structs and AppendData keep their flat compaction_key/
  compaction_rank columns -- the nesting is produce-surface only.
- Consumer.Register keeps its five params (the four required mirror
  Producer.Register plus group; bindings stays the one nil-able optional);
  NewConsumerInstance unexported instead -- only Register called it, so
  the 7-param public signature just disappears.

## Consequences

- A maybe-keyed produce path now branches to build its options (labs that
  published unkeyed baselines through a keyed helper grew that branch) --
  the branch is the design making "not compacted" explicit.
- CLI conn.go owns a connection parse-result struct; missing user/host/
  database in a URL fail as usage errors at parse, and the constructor
  backstops embedders at dial.
- Verified: all six module trees build + vet, root tests pass, and
  compactionlab, compactionranklab, cronlab, deferlab pass live.
