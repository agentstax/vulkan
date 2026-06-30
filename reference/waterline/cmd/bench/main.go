// Command bench measures the reference waterline implementation against the
// SQL/pgbench targets from the at-scale audit (bench/scale/results/FINDINGS.md):
//
//	happy path (claim-from-log, multi-group aggregate) : ~460-770k units/s
//	single hot group, frontier-bound                   : 136k -> 521k with K=16 lanes
//	exception drain (pop-delete, b1000)                : ~258k units/s
//
// A Go/pgx implementation runs LOWER than raw pgbench (driver + per-statement
// round-trips, and the happy path here also TRANSFERS every payload to Go).
// "Roughly meets" means same order of magnitude and the same RATIOS hold:
// claim-from-log >> per-row, sharded > single-frontier, pop-delete is the
// exceptional-only fallback. This harness prints all four numbers so the gap is
// visible and explainable.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/reference/waterline"
)

func dsn() string {
	if v := os.Getenv("WATERLINE_DSN"); v != "" {
		return v
	}
	return "postgres://bench:bench@localhost:5433/bench"
}

func main() {
	events := flag.Int("events", 1_000_000, "log size to seed (events)")
	workers := flag.Int("workers", 16, "concurrent happy-path/exception workers")
	batch := flag.Int("batch", 500, "claim batch size (happy path)")
	lanes := flag.Int("lanes", 16, "lanes for the sharded single-hot-group run")
	groups := flag.Int("groups", 8, "groups for the multi-group aggregate run")
	excN := flag.Int("exceptions", 500_000, "exception rows to drain (pop-delete run)")
	excBatch := flag.Int("exc-batch", 1000, "exception claim batch")
	flag.Parse()

	ctx := context.Background()
	l, err := waterline.New(ctx, dsn())
	if err != nil {
		die("connect", err)
	}
	defer l.Close()
	if err := l.Migrate(ctx); err != nil {
		die("migrate", err)
	}

	fmt.Printf("== seeding %d events (COPY) ==\n", *events)
	t0 := time.Now()
	head, err := l.AppendBatch(ctx, *events, []byte(`{"x":1}`), nil, nil)
	if err != nil {
		die("seed", err)
	}
	seedRate := float64(head) / time.Since(t0).Seconds()
	fmt.Printf("   append: %s units/s (target: 21k single-row .. 922k batched)\n\n", commas(seedRate))

	// 1) single-lane happy path: one group, one cursor row, W workers contend on
	//    the frontier (the 136k single-hot-group case).
	resetCursors(ctx, l)
	must(l.EnsureGroup(ctx, "single"))
	r1 := runHappy(ctx, l, "single", 1, *workers, *batch)
	report("happy single-lane (frontier-bound)", r1, "~136k SQL")

	// 2) sharded happy path: one hot group, K frozen-block lanes (the escape hatch).
	resetCursors(ctx, l)
	if _, err := l.InitLanes(ctx, "sharded", *lanes); err != nil {
		die("initlanes", err)
	}
	r2 := runHappy(ctx, l, "sharded", *lanes, *workers, *batch)
	report(fmt.Sprintf("happy sharded K=%d (escape hatch)", *lanes), r2, "136k->521k SQL")

	// 3) multi-group aggregate: G independent groups each draining the whole log.
	resetCursors(ctx, l)
	for g := 0; g < *groups; g++ {
		must(l.EnsureGroup(ctx, fmt.Sprintf("grp%d", g)))
	}
	r3 := runMultiGroup(ctx, l, *groups, *workers, *batch)
	report(fmt.Sprintf("happy multi-group G=%d (aggregate)", *groups), r3, "460-770k SQL")

	// 4a) exception drain, per-message Ack (one commit/fsync per message — the
	//     Phase 3.5 commit wall, here in its worst form).
	resetCursors(ctx, l)
	parkExceptions(ctx, l, "exc", *excN)
	r4 := runExceptionDrain(ctx, l, "exc", *workers, *excBatch, false)
	report("exception drain, per-message Ack", r4, "fsync-bound")

	// 4b) exception drain, batched AckBatch (B successes -> 1 commit). This is the
	//     faithful comparison to the SQL "pop-delete b1000" number.
	resetCursors(ctx, l)
	parkExceptions(ctx, l, "exc", *excN)
	r5 := runExceptionDrain(ctx, l, "exc", *workers, *excBatch, true)
	report("exception drain, batched pop-delete", r5, "~258k SQL")

	// 4c) fused pop-delete (claim+delete in one statement, no lease) — the exact
	//     primitive the SQL benchmark measured.
	resetCursors(ctx, l)
	parkExceptions(ctx, l, "exc", *excN)
	r6 := runFusedPopDelete(ctx, l, "exc", *workers, *excBatch)
	report("exception drain, fused pop-delete", r6, "~258k SQL")
}

func runFusedPopDelete(ctx context.Context, l *waterline.PgLog, group string, workers, batch int) result {
	var popped atomic.Int64
	var wg sync.WaitGroup
	t0 := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				evs, err := l.DrainPopDelete(ctx, group, batch)
				if err != nil {
					fmt.Fprintln(os.Stderr, "popdelete:", err)
					return
				}
				if len(evs) == 0 {
					return
				}
				popped.Add(int64(len(evs)))
			}
		}()
	}
	wg.Wait()
	return result{popped.Load(), time.Since(t0)}
}

type result struct {
	units   int64
	elapsed time.Duration
}

func (r result) rate() float64 { return float64(r.units) / r.elapsed.Seconds() }

// runHappy drains one group (lanes 1..K) with W workers until caught up.
func runHappy(ctx context.Context, l *waterline.PgLog, group string, lanes, workers, batch int) result {
	var processed atomic.Int64
	var wg sync.WaitGroup
	t0 := time.Now()
	for w := 0; w < workers; w++ {
		lane := w % lanes
		wg.Add(1)
		go func(lane int) {
			defer wg.Done()
			for {
				r, evs, err := l.Claim(ctx, group, lane, batch, 60*time.Second)
				if err != nil {
					fmt.Fprintln(os.Stderr, "claim:", err)
					return
				}
				if r.Empty() {
					return
				}
				processed.Add(int64(len(evs)))
				if err := l.Commit(ctx, r, nil); err != nil {
					fmt.Fprintln(os.Stderr, "commit:", err)
					return
				}
			}
		}(lane)
	}
	wg.Wait()
	return result{processed.Load(), time.Since(t0)}
}

// runMultiGroup runs G groups concurrently, workers split across groups.
func runMultiGroup(ctx context.Context, l *waterline.PgLog, groups, workers, batch int) result {
	var processed atomic.Int64
	var wg sync.WaitGroup
	t0 := time.Now()
	for g := 0; g < groups; g++ {
		group := fmt.Sprintf("grp%d", g)
		perGroup := workers / groups
		if perGroup < 1 {
			perGroup = 1
		}
		for w := 0; w < perGroup; w++ {
			wg.Add(1)
			go func(group string) {
				defer wg.Done()
				for {
					r, evs, err := l.Claim(ctx, group, 0, batch, 60*time.Second)
					if err != nil {
						fmt.Fprintln(os.Stderr, "claim:", err)
						return
					}
					if r.Empty() {
						return
					}
					processed.Add(int64(len(evs)))
					if err := l.Commit(ctx, r, nil); err != nil {
						fmt.Fprintln(os.Stderr, "commit:", err)
						return
					}
				}
			}(group)
		}
	}
	wg.Wait()
	return result{processed.Load(), time.Since(t0)}
}

func runExceptionDrain(ctx context.Context, l *waterline.PgLog, group string, workers, batch int, batched bool) result {
	var acked atomic.Int64
	var wg sync.WaitGroup
	t0 := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				ds, _, err := l.ClaimExceptions(ctx, group, batch, 100, 60*time.Second)
				if err != nil {
					fmt.Fprintln(os.Stderr, "claimexc:", err)
					return
				}
				if len(ds) == 0 {
					return
				}
				if batched {
					n, err := l.AckBatch(ctx, ds)
					if err != nil {
						fmt.Fprintln(os.Stderr, "ackbatch:", err)
						return
					}
					acked.Add(n)
					continue
				}
				for i := range ds {
					if err := l.Ack(ctx, &ds[i]); err != nil {
						fmt.Fprintln(os.Stderr, "ack:", err)
						return
					}
					acked.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	return result{acked.Load(), time.Since(t0)}
}

// parkExceptions bulk-inserts n ready deliveries (the exceptional fraction).
func parkExceptions(ctx context.Context, l *waterline.PgLog, group string, n int) {
	const q = `INSERT INTO deliveries(consumer_group,"offset",state)
	           SELECT $1, g, 'ready' FROM generate_series(1,$2) g`
	if _, err := l.Pool.Exec(ctx, q, group, n); err != nil {
		die("park", err)
	}
}

func resetCursors(ctx context.Context, l *waterline.PgLog) {
	if _, err := l.Pool.Exec(ctx, `TRUNCATE cursors, leases, deliveries`); err != nil {
		die("reset", err)
	}
}

func report(name string, r result, target string) {
	fmt.Printf("%-40s %s units/s  (%d units in %.2fs)  [target %s]\n",
		name, commas(r.rate()), r.units, r.elapsed.Seconds(), target)
}

func commas(f float64) string {
	n := int64(f)
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

func must(err error) {
	if err != nil {
		die("op", err)
	}
}

func die(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	os.Exit(1)
}
