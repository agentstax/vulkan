# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Migration LOCK TIMEOUT [0579]

1. Machinery (pkg/migrate/controller/datastore) — DONE 2026-08-22:
   - `ddlLockTimeout = 2 * time.Second` const in step.go — own copy,
     matching the producer/janitor sites; no config field.
   - runStepWithTx executes `SET LOCAL lock_timeout` right after Begin —
     one site covers Up/Down, Validate, and recordSuccess.
   - errors.go: `errStepLockTimeout` = VK0053 Transient + the
     isLockNotAvailable helper (55P03); runStep reclassifies on the txn
     path only — NoTxn keeps fail-fast.
   - Migration doc comment (pkg/migrate/migration.go) gains the
     authoring-rules line: txn steps run under a 2s lock_timeout, a wait
     past it retries; NoTxn steps get no cap.
2. Docs:
   - website/src/content/docs/errors/VK0053.md, headed by the verbatim
     problem text.
   - guides/migrations.mdx: state the lock_timeout behavior.
3. Verify: build, `go test -race` on pkg/migrate/..., schemaevolutionlab +
   schemagatelab.
