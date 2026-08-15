---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0518 — Config is code-owned, the latest declaration wins, and the CLI never writes config

**Context.** Topic config had two owners: RegisterTopic re-asserted a full
TopicConfig from app code and raised ErrTopicConfigMismatch on disagreement,
while AlterTopic let operators change those same columns -- so an operator's
change broke the next deploy, and [0515] solved the same conflict for groups a
third way with {default, override}. Research across systems that carry two
writers: Postgres, Kafka and git config resolve one axis only (scope
specificity) and have no code-declared layer at all; K8s needed per-field
managers because its writer set is open-ended, and managedFields is widely
disliked; RabbitMQ's precedence carries per-field exceptions and a conservative
merge, and generates recurring "policy not applied" confusion; Ansible's 22
levels and Puppet's Hiera both converge on "which layer set this" as the
dominant support burden. Vulkan's deployment model is one team, one database,
a few apps -- the operator and the developer are the same person, so the
authority axis is an org artifact its users do not have.

**Decision.**
- Config is declared in code. The latest declaration wins. The CLI reads
  config and never writes it.
- Topic mutable config (retention_ttl, allow_drop_past_committed,
  idempotency_key_ttl, delivery_log_mode) is overwritten by each
  RegisterTopic. A register whose values differ from stored logs old -> new at
  Info: two apps declaring one topic differently is the only mistake this model
  cannot prevent by construction, and that line -- later the declaration trail,
  [0519] -- is how it gets found.
- partition_size stays compared and keeps raising ErrTopicConfigMismatch.
  Partition tables and their ranges are derived from it, so a change wedges the
  producer heal path (CREATE TABLE IF NOT EXISTS no-ops on the colliding index
  name) and defeats the janitor's allow_drop_past_committed guard
  (lastIdInPartition computed on the wrong grid drops uncommitted ids).
  Changing it is a topic migration, not a config write.
- Group and worker mutable config stays in worker metadata and loses the
  override layer -- one value, written by the declaring code at each Register.
- Producers, consumers and workers resolve topics with GetTopic.
  RegisterTopic belongs to admin.
- The CLI keeps verbs and inspection (register, destroy, rename, migrate,
  suspend, resume, run, scale, get, list). Only config mutation leaves.

**Consequences.** Supersedes [0515] and [0517]. Deleted: AlterTopic,
AlterGroup, AlterWorker(s), their Alter*Config types, MetadataValue's Override
layer, applyOverrides, pkg/common/update.go, and `config set` / `config unset`
for every resource (`config get` stays). ErrTopicConfigMismatch narrows from
five columns to one. Changing any mutable config field now requires a redeploy,
which for a team owning both the app and the database costs minutes and buys a
git history. Rejected: scope chains of system/topic/group/worker defaults --
with a handful of topics a shared default is a Go variable in the user's code, and
reimplementing variable scoping inside a database is worse than composition;
operator overrides at any scope, whose whole justification disappears once the
operator and the developer are one person. Adding an operator writer later is
additive, while removing one after users depend on it is breaking, so pre-v1
takes the retractable direction.
