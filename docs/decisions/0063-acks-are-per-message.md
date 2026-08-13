---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0063 — Claim per batch, record success or failure per message

**Context.** Batching the claim amortizes a round-trip over many rows, but an earlier design that claimed and processed a batch inside one transaction made the whole batch a single failure domain: one bad message rolled back its unrelated batch-mates.

**Decision.** The claim is its own fast transaction (flip `status` to `processing`, stamp `lease_until`, increment `attempts` for the batch), and each message is then processed and recorded individually — a single-row `UPDATE … WHERE id=$1 AND lease_token=$2` per message.

**Consequences.** A failure lands only on its own row, driving that row's backoff, retry, or dead-letter path; one bad message cannot poison its batch-mates. The batch is purely a round-trip optimization, not a failure unit. Cost: one commit per message on the success/failure path, which becomes the throughput ceiling addressed by the `synchronous_commit` work.
