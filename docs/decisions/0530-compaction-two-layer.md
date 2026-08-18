---
status: accepted
date: 2026-08-18
phase: 14b
---

# pkg/compaction stays two-layer; MessageRow lives in common

## Context

Every other domain follows pkg/<x> -> pkg/<x>/controller ->
pkg/<x>/controller/datastore. Compaction has no pkg/compaction vocabulary
package; its read-model is common.MessageRow. The domain is also co-owned:
the head upsert and the FOR UPDATE head read inside a produce transaction
belong to the producer stack, while pkg/compaction/controller is the plain
read door. Producer (GetCompactionHeadInTx, aliased through
producer/controller), alert's classify, and the compaction controller all
return or take MessageRow.

## Decision

- Compaction stays two-layer as a deliberate exception: it owns no
  vocabulary, so no pkg/compaction package exists to hold none.
- MessageRow stays in pkg/common. It is shared across the producer and
  compaction stacks, and common is the sanctioned home for vocabulary shared
  across different stacks (same rule that places shared error sentinels
  there).
- Moving MessageRow into a new pkg/compaction was rejected: producer would
  import another domain's vocabulary for a row the producer itself writes,
  and the package would exist to hold exactly one moved struct.

## Consequences

- The three-layer template stays the default; compaction is the one recorded
  exception, revisited only if compaction grows vocabulary of its own
  (sentinels, enums, a second read-model).
- Callers keep common.MessageRow (or the producer alias) in signatures; no
  import changes anywhere.
