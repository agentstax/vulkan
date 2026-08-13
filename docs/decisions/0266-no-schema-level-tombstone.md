---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0266 — Key deletion is expressed in the payload; there is no schema-level tombstone

**Context.** Kafka defines a protocol-level tombstone marker so its background compactor — which physically deletes rows — can recognize a deletion generically across topics without understanding payload schemas, and eventually purge the tombstone itself.

**Decision.** No tombstone concept in the schema. "How do I delete a key" is answered entirely by what the producer puts in `payload` (for example an app-defined `Deleted bool` field). The claim-time filter already returns whatever the latest row is with zero special-casing, regardless of its contents, and nothing here physically deletes rows — so Kafka's motivating reason does not apply.

**Consequences.** A tombstoned key still delivers its latest row on both claim paths like any other. Cost accepted: a future generic disk-space cleanup pass cannot recognize "this key is fully dead, purge every row for it" without understanding each topic's payload schema — real, but currently unneeded. **Rejected:** a schema-level tombstone marker — machinery for a background deleter this design does not have.
