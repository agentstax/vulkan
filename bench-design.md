# Benchmark pipeline — vulkan design notes

Companion to `bench-methodology.html` (the generic method, rules R1–R14).
This doc maps the method onto vulkan: settled decisions, the record schema,
harness shape, and the fold-in inventory for the five existing benches.
Design round 2026-08-22; ROADMAP item 14c. Root planning doc — folds into
the record-keeping surface (decision records + TODO.md) when design closes.

**Status: tabled 2026-08-22** for the documentation-first pass — the
user-facing spec (the methodology page, ported into website/) gets written
and reviewed before the harness is built; documentation drives the
implementation. bench-methodology.html is the draft of that doc-site page.

## 1. Settled this round

- **Two tiers.** Tier 1: CPU-side library paths via `go test -bench` emitting
  the standard Go benchmark format, compared with `benchstat` (median +
  Mann-Whitney, `-count=10` minimum). Tier 2: anything touching Postgres —
  the bench/ system harness below. Don't build a custom pipeline where the
  Go toolchain already is one.
- **Recording = git-tracked append-only JSONL** per bench
  (`bench/<name>/results/cells.jsonl`), one self-describing record per
  cell. A *cell* is one execution of a bench at one point of its
  parameter matrix — the existing harnesses' own term (`sweep.sh`'s
  `cell()` function, `cells.jsonl`), kept here because the code already
  speaks it; the generic methodology page says "run" and "record" instead.
  No results database now (one dev, one machine, no reader) — but records
  are keyed like rustc-perf/Conbench rows so a loader or Apache Otava can
  ingest history later. Reverses idempotency's gitignore call: the harness
  and raw records get tracked everywhere.
- **Latency histograms: direct dep** on `github.com/HdrHistogram/hdrhistogram-go`
  in the bench module (dev-only module; the no-new-deps rule guards the
  main module).
- **No regression detection yet.** The report tool does benchstat-style
  comparison with honest "no significant difference" output. Change-point
  detection (Otava) is a later phase, unlocked by the raw-samples record.
- **Cross-time comparisons are advisory; claims are same-session.** One
  developer machine means environment drift is real. A/B claims interleave
  arms in one session (R13); the recorded fingerprint is what lets us
  exonerate the code when a trend line jumps.

## 2. The result record (draft)

One JSON line per cell. Three blocks; the identity block is the series key.

Identity (determines history — a change here forks the series):
- `bench` — directory name (`compaction`, `multitopic`, …)
- `cell` — label + every workload parameter, flat keys in the bench's own
  vocabulary (`producers`, `goroutines`, `batch_limit`, `groups`,
  `message_bytes`, `cardinality`, `failure_rate`, …)
- `mode` — `saturation` | `rate` (R6); `rate` cells add `target_rate`
- `sync` — `synchronous_commit` **read back from the server** (R4)
- `environment` — human-assigned name for the pinned setup (the
  container.sh spec has a name; renaming it deliberately resets history)

Provenance (informational — recorded, not identity):
- `recorded_at` (RFC3339), `library_sha` (+ `-dirty`), `go_version`,
  `pg_version`, `container` (image + cpus + memory + GUC overrides),
  `host` (CPU model, cores, RAM, OS)

Measurements:
- `samples` — the raw per-second array for the window (R12); summaries
  (`med`, `p10`, `p90`) computed from it by the tools
- latency (rate cells): `p50/p90/p99/p999/max` in ms decoded for
  readability + the merged HdrHistogram base64-encoded for re-analysis
- `pg` — before/after deltas: WAL bytes+records+FPI per message,
  checkpoints (`pg_stat_checkpointer`), `n_tup_hot_upd`/`n_tup_upd`,
  `n_dead_tup`, relation sizes, deadlocks (R9)
- `guards` — named booleans, all must hold (R10): e.g.
  `backlog_bounded`, `no_deadlocks`, `stable_window` (the scale bench's
  diverging/decay verdicts, standardized), `generator_headroom` (R5),
  `schedule_kept` (rate cells: the driver never fell behind its intended
  send schedule — a slipped schedule reintroduces coordinated omission)
- `limiter` — required free-text naming the saturated resource, with the
  counter that proves it (R11)
- `reps` — rep index when a cell is one of N fresh executions (R13)

## 3. Harness shape (Tier 2)

- One shared package in the bench module (name settles at build time):
  open-loop fixed-rate driver with precomputed intended-send timestamps,
  the warmup/window phase machine, histogram merge, environment capture,
  guard evaluation, record writer. The compaction/fillfactor driver
  pattern promoted to the standard.
- Each bench = a directory with a small `main.go` declaring workload
  parameters and the produce/consume closures, driving the **real library
  API** (R1) with fresh-topic-per-cell isolation
  (`RegisterTopic("<bench>.<unixnano>")` + deferred destroy).
- One shared `bench/env.sh` (the existing :5433 convention) and one shared
  `bench/container.sh` (pinned `postgres:18.4`, `--cpus=8 --memory=8g`,
  pinned GUCs) — promoted from the compaction/fillfactor copies (R3).
- A `report` tool (Go, replaces `scale/analyze.py`): renders cells.jsonl
  into tables, aggregates reps (median-of-medians — never by hand, R12),
  computes A/B deltas with spread.
- `just` recipes: `bench-<name>`, `bench-report` (currently zero bench
  recipes vs ~45 lab recipes).
- `RESULTS.md` per bench stays the human layer (R14), compaction/fillfactor
  house style: environment paragraph → method paragraph → pointer to raw
  cells → small tables → `## Conclusions` citing decision records.

## 4. Tier 1 first consumer: debug-buffer overhead

The [0559] adoption gate for always-on capture. Note [0565] reshaped the
mechanism: `BufferLogger` no longer exists — the cost to measure is
`logging.NewPipelineLogger` with `Buffer` on vs off, per operation on the
healthy path (capture + no-drain). Plain `go test -bench` in the owning
package's test file, benchstat comparison, number lands in the docs page
that gates the decision.

## 5. Postgres-specific method notes (feeds R3/R9)

- Windows for sustained claims span ≥1 checkpoint cycle; report
  `pg_stat_checkpointer` deltas so a checkpoint-free window is visible.
- Autovacuum stays **on** with settings disclosed for any published number
  (a queue table is an autovacuum torture test; disabling it is this
  domain's page-cache-durability lie). Explicit `avoff` cells remain legal
  as labeled diagnostics (idempotency phase A style).
- `synchronous_commit=on` is the headline posture; `off` cells are labeled
  diagnostic (R14). `fsync=off` never appears in a published number.
- Container distortions to avoid: PGDATA on a volume (never overlayfs),
  no CPU-quota throttling (cgroup throttling injects ~100ms stalls that
  read as p99 spikes), replace the image's 128MB `shared_buffers` default.
  Docker/OrbStack overhead itself is ~1.5% and acceptable with disclosure.
- Characterize the storage once per environment with `pg_test_fsync`;
  record it in the environment fingerprint.
- Driver co-tenancy (generator and Postgres on one machine) is our
  reality; R5 makes generator CPU part of every record so the co-tenancy
  is priced, not hidden.

## 6. Fold-in inventory (from the bench/ audit, 2026-08-22)

Per-bench state:

| bench | drives | recorded | fold-in notes |
|---|---|---|---|
| compaction | real API | cells.jsonl + RESULTS.md | closest to the standard; add fingerprint + guards fields |
| fillfactor | real API | cells.jsonl + RESULTS.md | same; its drain guard becomes the standard `backlog_bounded` |
| idempotency | SQL mirror | RESULTS.md only (harness gitignored) | re-home on the real Produce path (R1); un-gitignore; WAL-per-message capture is the keeper |
| scale | SQL mirror + projector | jsonl + 3 overlapping writeups | design-exploration bench, mostly historical; keep the diverging/decay verdicts + audit habit; its jsonl stays as history |
| trigger_fanout | synthetic schema | nothing | results were never stored; retire or re-run under the standard if the question still matters |

Concrete debts the unification clears:
- `trigger_fanout/run.sh` targets the **dev DB on :5432** (`example_user`)
  — every other bench targets the throwaway :5433; must move or retire.
- `bench/projector` — stale 13MB tracked binary at a path `measure.sh`
  doesn't even read (`scale/projector/projector` is what it looks for);
  delete, `go build` produces it.
- Four near-identical `env.sh` copies → one shared file; two identical
  `container.sh` copies → one shared file.
- Three config idioms (env vars / Go flags / positional args) → Go flags,
  the driver pattern's idiom.
- Same concept, three names (`PC` / `-producers` / `CLIENTS`) → the
  harness package's flag names, spelled out per conventions.
- Warmup/window defaults differ per bench and even per phase → the
  harness owns defaults; cells echo actuals into the record.
- `scale`'s `ALTER SYSTEM RESET ALL` cleanup nukes unrelated cluster
  settings — the harness never uses ALTER SYSTEM; per-database or
  per-table settings only, restored per cell.
- Aggregation by hand in prose (median-of-medians in markdown) → the
  report tool (R12).
- No bench records SHA/date/host → fingerprint block (R4).

## 7. First Tier-2 workload: multi-topic throughput/latency

The roadmap's named first consumer. Multi-topic, high concurrency, pushed
to real DB limits (connection pool, lock table, I/O) rather than the
library's own bottleneck. Under the method: a saturation ladder to find
sustainable max (R7), then rate cells at fractions of it for the latency
spectrum (R6/R8), windows spanning checkpoints (R9), per-cell limiter
analysis (R11). Workload details are design work for its own session —
this doc only fixes the method it runs under.

## 8. Open: first-build scope

Options for what "core in place" means, for discussion:

- **A — record + harness + one workload.** Shared package, record schema,
  env/container promotion, report tool, multi-topic workload as the
  proving consumer, method doc finalized. Legacy fold-in deferred.
- **B — A minus the workload.** Land the harness + schema + tooling,
  prove it by porting fillfactor or compaction (smallest real consumer),
  multi-topic workload as the next piece of work.
- **C — A plus Tier 1.** Also the debug-buffer microbench ([0559] gate),
  since it's small and unblocks a parked decision.

Recommendation: **B, then the [0559] microbench, then multi-topic.**
Porting an existing bench proves the harness against known numbers before
any new claim depends on it; the microbench is nearly free; the multi-topic
workload then lands on a proven pipeline instead of debugging both at once.

Scope was not settled when the round was tabled; the docs-first pass may
reshape it (the spec page may become the first deliverable of any option).

Other open threads from the round:
- **"cell" naming** — kept in this doc (it is the existing harnesses' own
  term) and defined in §1; removed from the generic methodology page in
  favor of run/record. Open: whether the harness rebuild renames it away
  entirely (`cells.jsonl` → `records.jsonl`, `cell()` → plainer) under the
  no-coined-shorthand rule — cheap to do since every harness gets touched.
- **Secondary sources** — methodology-page refs 7 (InfoQ on MongoDB
  defaults) and 9 (HN on the TechEmpower sunset) are secondary; if the
  published page holds a primary-sources-only bar, soften or drop those
  two claims.

## 9. Research source index (so nothing is lost)

Pipelines: Go benchfmt spec + x/perf (benchfmt/benchproc/benchmath
importable) · rustc-perf schema.md + comparison-analysis.md (noise-derived
IQR thresholds) · Conbench (identity/informational split,
history_fingerprint, lookback z-score, Postgres schema with raw-sample
arrays) · MongoDB CPD paper arXiv:2003.00584 (~99% threshold false
positives; triage state machine) · Hunter/Otava (E-divisive + t-test;
first-class Postgres source, writes change points back) · asv (in-repo
JSON results, step detection) · github-action-benchmark (the
one-number-per-run cautionary tale) · LNT (pairwise-only, drowned in noise).

Methodology: Tene (coordinated omission, HdrHistogram, intended send
time) · Schroeder NSDI'06 (open vs closed) · Barrett OOPSLA'17 (warmup
must be verified; ≤43% reach steady state) · Kalibera & Jones ISMM'13
(repetition levels) · Mytkowicz ASPLOS'09 (measurement bias; don't claim
effects smaller than your rig's swing) · Gregg (active benchmarking, USE)
· SIGPLAN checklist · benchstat model (median + Mann-Whitney + "~").

Domain: OMB workload vocabulary + the Kafka/Redpanda/Pulsar wars
(Vanlightly's teardown: fsync asymmetry, 4-producer cherry-pick, fresh-SSD
honeymoon) · pgbench docs (rate mode = CO done right; short runs lie;
scale-vs-clients trap) · ClickBench (one-script rerun, tuned-labeled,
self-declared limitations) · TigerBeetle (benchmark ships in product) ·
hardbyte/postgresql-job-queue-benchmarking (closest prior art: contract
classification, chaos phases, dead-tuple/WAL/recovery metrics) ·
Postgres-in-Docker distortion list (volume, cgroup throttle, defaults).
