# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Table + column rename, message-key promotion [0611][0612][0613]

Regenerate `just schema-diagram` after every schema chunk; final shape
review on the diagram before close-out.

### 1. Mechanical rename sweep (0611 + 0613, minus cron and the key) -- DONE 2026-08-29

Tables: system/topic/consumer_group/worker/binding gain `_config`
(+ logs `_config_log`), delivery->exception_queue,
cursor->consumer_group_cursor, lease->claim_lease. `key_lease` waits
for chunk 4; cron tables wait for chunk 2.
Columns: binding_log attempt_at->attempted_at, lease until->expires_at,
delivery lease_until->lease_expires_at, binding display->pattern +
pattern->pattern_regex.

- baseline DDL in both tables.go files (pre-v1: edit in place, verify
  drop+recreate)
- internal/topic table-name funcs and their callers
- every SQL literal naming a renamed table/column (~11 pkg files for
  topic alone); `-- vulkan:` owner comments untouched
- labs: 36 files interpolate per-topic names inline; grep for query
  mirrors [lab-query-staleness]
- scripts/database/tbls.yml include list + relations
- verify: build, `go test -race` touched packages, drop+recreate,
  directly-affected labs
- verified: builds all modules, vet, pkg + tools tests, fresh DB
  recreate + diagram, labs consumergrouplab / deletetopiclab /
  bindinglab / reclaimlab / destroysystemlab / deferlab green.
  deferlab failed ONCE ("unkeyed run must not hold a key lease",
  leaseCount raced a keyed sibling's lease under concurrency) then
  passed on rerun -- watch at the full-suite checkpoint; looks like
  pre-existing raciness, the sweep is name-only

### 2. cron_job_cursor split + cron renames (0611 + 0613 cron scope) -- DONE 2026-08-29

No cron doc page exists on the site, so there was nothing to gate on;
[0613] served as the spec directly.

- cron_job->cron_job_config; new 1:1 cron_job_cursor takes
  next/last_scheduled_time as next/last_scheduled_at + the due index
- scheduler datastore: due scan joins config for schedule/suspended;
  schedule edit becomes two-table write (recompute next)
- metrics reads next_scheduled_time (pkg/metrics measurement/adapters)
  -- check for wire/json exposure before renaming
- cron_job.data->payload: columns, Go fields, wire tags, declare path;
  grep labs for `->>'data'`
- verify: cronlab, alertlab, metrics labs
- verified: cron_job_config + cron_job_cursor live (register writes
  both rows; unsuspend/replace are two-table transactions; due scan
  and claim join; suspend splits config/cursor writes). Also renamed
  under [0613]'s rule: JobRequest.ScheduledTime->ScheduledAt (wire
  scheduled_at, caught by cronlab's `->>'scheduled_time'` NULL),
  admin RegisterCronJob's data param->payload, alert
  JobData->JobPayload chain (alert.Alert.Data left, flagged in the
  record). Labs cronlab / alertlab / metricscollectorlab green
  (metrics lab needs `just metrics-collector-lab` or a rebuilt
  bin/vulkan -- stale binary was a false failure)

### 3. 0612 doc page (the proposal -- gate for chunk 4)

- produce + compaction pages rewritten around "the message key":
  MessageKey on ProduceOptions, CompactionOptions{Rank} opt-in,
  Defer needs only a key; new sections marked Proposed [0581]
- find the defer-requires-key declared error; draft its reworded
  problem/fix text and page edit
- VOICE.md pass; user reviews before any chunk-4 code

### 4. 0612 implementation + message_key_lease

- ProduceOptions.MessageKey; CompactionOptions loses Key;
  NewCompactionOptions signature; Compaction-without-MessageKey errors
- MessageRow.CompactionKey->MessageKey, wire compaction_key->message_key;
  columns in message_log + key_lease; key_lease->message_key_lease
- defer without compaction: key-lease claim path must not assume a
  compaction head; supersede logic untouched when Compaction nil
- labs: deferlab extends to the uncompacted case; grep labs for
  `->>'compaction_key'`; alert.CompactionKey helper + callers
- error/event docs pages whose text says compaction key

### 5. Conventions enforcement + rule files

- tools/conventions walk over the baseline DDL literals: timestamptz
  columns end _at/_after, duration columns end _ns, table names end in
  a known kind; sabotage-test it before trusting it
- CONVENTIONS ## Tables gains the [0611] naming rule + [0613] column
  rules; ## Vocabulary rows: compaction key -> message key
- doc-site prose sweep for old table/column names (website/, VOICE.md
  pass on touched pages)

### 6. Close-out checkpoint

- full fresh-DB lab suite; `just schema-diagram-fresh` review on the
  final diagram
- HISTORY.md entry citing [0611][0612][0613]; remove this section and
  the ROADMAP item; memory update
