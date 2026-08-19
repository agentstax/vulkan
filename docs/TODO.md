# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Config & options refinement (from ROADMAP Now; picked up 2026-08-19)

Decisions first, sweeps after — the sweeps touch every config file, so the
pattern decisions must settle before any file moves.

- [x] The three shape decisions settled and built [0542]: Config name kept
      + CONVENTIONS.md rule + PostgresConnectionConfig fix; Compaction
      nested into ProduceOptions.Compaction (NewCompactionOptions);
      Register kept at 5 params, NewConsumerInstance unexported.
- [ ] Sweep: group/order config fields and table fields by likeness.
- [ ] Sweep: pass through config/options/vars/params, delete dead fields.
