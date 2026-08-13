---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0243 — topic_id joins the keys of cursor/deliveries/binding; lease gets the column only

**Context.** With each topic owning its own id sequence, a bare `message_id`
can refer to completely different messages in two topics' tables, and a
group's `claimed`/`committed` are meaningless without knowing which
`message_log_<topic_id>` sequence they count against.

**Decision.** `cursor`'s PK became `(consumer_group, topic_id)`,
`deliveries`' PK became `(consumer_group, topic_id, message_id)`, and
`binding` gained `topic_id` alongside `consumer_group`/`pattern` with its
index widened — each folded into that table's original migration. `lease`
gained the `topic_id` column without a key change, because the lease `token`
is already a unique random id that disambiguates a row on its own; the
column is there only to make what the lease tracks unambiguous.

**Consequences.** Every table that references message ids or per-log
positions names its topic explicitly. Key width is paid only where identity
actually requires it — `lease` shows the rule: a column for clarity is not
the same decision as a column in the key.
