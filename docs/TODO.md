# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## worker_log — worker metadata history ([0577])

- [x] DDL: worker_log table + worker_log_worker (worker_id, id) index in
      createSystemTables, beside worker.
- [x] Datastore: registerWorker restructured onto replaceConfig's
      decide-before-writing shape — insert path appends the log row in its
      transaction; conflict path reads the row with server-side jsonb
      equality, returns without writing when unchanged, else UPDATE + log
      append in one transaction. appendWorkerLog snapshots the worker row
      server-side (INSERT ... SELECT). RegisterWorker gains declaredBy.
- [x] Controller: RegisterWorker passes common.ProcessIdentity down.
- [x] Verify: build, go test -race on touched packages, directly-affected
      lab.
