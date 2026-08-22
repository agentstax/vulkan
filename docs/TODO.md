# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## CLI --output json ([0575] json tags, [0576] the flag)

Built 2026-08-22, awaiting review. Verified: cmd/vulkan build + vet + cli
tests (json error handler covered in errors_test), root-module build,
`go test -race` on every touched vocabulary package, tools/conventions,
cron-lab + alert-lab (the two labs whose SQL reads stored payload keys —
both updated to the new snake_case keys and passing).

- [x] Decision records 0575/0576, CONVENTIONS.md json-tag rule, ROADMAP
      item slimmed to a pointer.
- [x] Tag sweep: `json:"snake_case"` on every public read-model; registry
      keys where the log-attr table has one (Topic.Name -> "topic",
      SchemaVersion -> "version", SessionCounters -> *_count). Write
      shapes, configs, instances untagged. Note: JobRequest and Alert are
      stored payloads, so their stored key shape changed (pre-v1, fresh-DB
      verified; lab mirrors swept).
- [x] Seam: `--output` root persistent flag (text|json, validated in the
      root PersistentPreRunE -> usage error), errorHandler json branch on
      stderr, writeJSON + jsonAttrValue in json.go (durations render with
      units).
- [x] Read commands: topic list/get, topic config get, group config get,
      cron list/get, alert list/bindings, metrics list/get, system get,
      migrate status/versions, explain. failPrinted failures are
      result-document data (exists:false), exit codes preserved.
      Composed/derived shapes got *Document structs beside their command;
      duration-free tagged read-models marshal directly.
- [x] Mutation commands: destroys emit what-happened records and require
      --yes in json mode; cron run emits {cron_job, message_id}; cron
      suspend/unsuspend emit {cron_job, suspended, next_scheduled_time};
      rename echoes the get-shape; migrate init/up/down emit a summary
      document. manager run rejects the flag (no result document).
- [x] Guards: -q with --output json = usage error on all 6 -q commands.

Close-out (after review, once the parallel worker_log work settles):
full fresh-DB lab suite, HISTORY.md entry citing [0575][0576], remove
this section + the ROADMAP pointer.
