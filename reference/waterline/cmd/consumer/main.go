// Command consumer runs the reference waterline consumer. It mirrors the
// LEARNING_PLAN failure-injection knobs (--sleep, --fail-rate, --crash-after)
// so it doubles as the plan's reference binary.
//
// Modes:
//
//	--mode happy  (default): the claim-from-log happy path (no per-event rows) +
//	              a pop-delete exception drain + a lazy Advance roller. --lanes>1
//	              runs the sharded escape hatch (call --init-lanes once first).
//	--mode fifo : the FIFO-partition lifecycle path (Phase 8): Materialize a
//	              delivery per event, then drain with the at-most-one-in-flight-
//	              per-key ordering gate.
//
// It prints (group, offset, partition_key) per processed message so ordering and
// routing are visible at a glance.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
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
	group := flag.String("group", "demo", "consumer group")
	mode := flag.String("mode", "happy", "happy | fifo")
	lanes := flag.Int("lanes", 1, "happy-path lanes (sharded escape hatch)")
	initLanes := flag.Bool("init-lanes", false, "(re)assign K frozen-block lanes for --group then exit")
	workers := flag.Int("workers", 4, "concurrent workers")
	batch := flag.Int("batch", 100, "claim batch size")
	maxAttempts := flag.Int("max-attempts", 3, "exception attempts before dead-letter")
	sleep := flag.Float64("sleep", 0, "artificial per-message processing time (seconds)")
	failRate := flag.Float64("fail-rate", 0, "artificial failure probability per message")
	crashAfter := flag.Int("crash-after", -1, "os.Exit(1) after processing N messages (simulate a crash)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	l, err := waterline.New(ctx, dsn())
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer l.Close()

	if *initLanes {
		head, err := l.InitLanes(ctx, *group, *lanes)
		if err != nil {
			fmt.Fprintln(os.Stderr, "init-lanes:", err)
			os.Exit(1)
		}
		fmt.Printf("assigned %d lanes for group %q over head=%d\n", *lanes, *group, head)
		return
	}
	if *lanes <= 1 {
		if err := l.EnsureGroup(ctx, *group); err != nil {
			fmt.Fprintln(os.Stderr, "ensure group:", err)
			os.Exit(1)
		}
	}

	var done atomic.Int64
	process := func(e waterline.Event) error {
		if *sleep > 0 {
			time.Sleep(time.Duration(*sleep * float64(time.Second)))
		}
		n := done.Add(1)
		if *crashAfter > 0 && n >= int64(*crashAfter) {
			fmt.Printf("crashing after %d messages\n", n)
			os.Exit(1)
		}
		if rand.Float64() < *failRate {
			return errors.New("artificial failure (--fail-rate)")
		}
		key := ""
		if e.PartitionKey != nil {
			key = *e.PartitionKey
		}
		fmt.Printf("[%s] offset=%d key=%s\n", *group, e.Offset, key)
		return nil
	}

	switch *mode {
	case "fifo":
		runFIFO(ctx, l, *group, *workers, *batch, *maxAttempts, process)
	default:
		runHappy(ctx, l, *group, *lanes, *workers, *batch, *maxAttempts, process)
	}
}

// runHappy: N happy-path workers (reclaim-then-claim) + one exception drain
// worker + a lazy Advance roller, all until SIGINT.
func runHappy(ctx context.Context, l *waterline.PgLog, group string, lanes, workers, batch, maxAtt int, process func(waterline.Event) error) {
	lease := 30 * time.Second
	const maxReclaims = 5 // quarantine a crash-looping batch after this many reclaims
	idle := func() { sleepCtx(ctx, 200*time.Millisecond) }

	spawn(ctx, workers, func(ctx context.Context, w int) {
		// Each worker round-robins over ALL lanes (starting at a different offset
		// so they spread out) so that workers < lanes still drains every lane — a
		// worker pinned to one lane would leave the others stuck.
		lane := w % lanes
		emptySweep := 0
		for ctx.Err() == nil {
			r, evs, reclaimed, err := l.Reclaim(ctx, group, lease, maxReclaims)
			if err != nil {
				logErr("reclaim", err)
				idle()
				continue
			}
			if !reclaimed {
				r, evs, err = l.Claim(ctx, group, lane, batch, lease)
				if err != nil {
					logErr("claim", err)
					idle()
					continue
				}
				lane = (lane + 1) % lanes // next lane next time
			}
			if r.Empty() {
				if emptySweep++; emptySweep >= lanes {
					idle()
					emptySweep = 0
				}
				continue
			}
			emptySweep = 0
			var exc []waterline.Exception
			for _, e := range evs {
				if perr := process(e); perr != nil {
					exc = append(exc, waterline.Exception{Offset: e.Offset, Err: perr.Error()})
				}
			}
			if err := l.Commit(ctx, r, exc); err != nil && !errors.Is(err, waterline.ErrLeaseLost) {
				logErr("commit", err)
			}
		}
	})

	// exception drain (pop-delete)
	go func() {
		for ctx.Err() == nil {
			if !drainExceptions(ctx, l, group, batch, maxAtt, lease, process) {
				idle()
			}
		}
	}()
	// lazy Advance roller (waterline GC, off the hot path)
	go rollAdvance(ctx, l, group, lanes)

	<-ctx.Done()
	fmt.Println("shutting down")
}

// runFIFO: a Materialize ticker + N FIFO-partition workers.
func runFIFO(ctx context.Context, l *waterline.PgLog, group string, workers, batch, maxAtt int, process func(waterline.Event) error) {
	lease := 30 * time.Second
	go func() {
		for ctx.Err() == nil {
			if n, err := l.Materialize(ctx, group); err != nil {
				logErr("materialize", err)
			} else if n > 0 {
				fmt.Printf("materialized %d deliveries\n", n)
			}
			sleepCtx(ctx, 200*time.Millisecond)
		}
	}()

	spawn(ctx, workers, func(ctx context.Context, _ int) {
		for ctx.Err() == nil {
			ds, evs, err := l.ClaimPartitioned(ctx, group, batch, maxAtt, lease)
			if err != nil {
				logErr("claim-partitioned", err)
				sleepCtx(ctx, 200*time.Millisecond)
				continue
			}
			if len(ds) == 0 {
				sleepCtx(ctx, 100*time.Millisecond)
				continue
			}
			byOff := map[int64]waterline.Event{}
			for _, e := range evs {
				byOff[e.Offset] = e
			}
			for i := range ds {
				e := byOff[ds[i].Offset]
				switch perr := process(e); {
				case perr == nil:
					must(l.Ack(ctx, &ds[i]))
				default:
					must(l.Nack(ctx, maxAtt, &ds[i], perr))
				}
			}
		}
	})
	go rollAdvance(ctx, l, group, 1)

	<-ctx.Done()
	fmt.Println("shutting down")
}

// drainExceptions claims and reprocesses one batch of exceptions; returns true
// if it did any work.
func drainExceptions(ctx context.Context, l *waterline.PgLog, group string, batch, maxAtt int, lease time.Duration, process func(waterline.Event) error) bool {
	ds, evs, err := l.ClaimExceptions(ctx, group, batch, maxAtt, lease)
	if err != nil {
		logErr("claim-exceptions", err)
		return false
	}
	if len(ds) == 0 {
		return false
	}
	byOff := map[int64]waterline.Event{}
	for _, e := range evs {
		byOff[e.Offset] = e
	}
	for i := range ds {
		e := byOff[ds[i].Offset]
		switch perr := process(e); {
		case perr == nil:
			if err := l.Ack(ctx, &ds[i]); err != nil && !errors.Is(err, waterline.ErrLeaseLost) {
				logErr("ack", err)
			}
		default:
			if err := l.Nack(ctx, maxAtt, &ds[i], perr); err != nil && !errors.Is(err, waterline.ErrLeaseLost) {
				logErr("nack", err)
			}
		}
	}
	return true
}

func rollAdvance(ctx context.Context, l *waterline.PgLog, group string, lanes int) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for lane := range lanes {
				if _, err := l.Advance(ctx, group, lane); err != nil {
					logErr("advance", err)
				}
			}
		}
	}
}

func spawn(ctx context.Context, n int, fn func(context.Context, int)) {
	for w := range n {
		go fn(ctx, w)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func logErr(what string, err error) { fmt.Fprintf(os.Stderr, "%s: %v\n", what, err) }

func must(err error) {
	if err != nil && !errors.Is(err, waterline.ErrLeaseLost) {
		logErr("op", err)
	}
}
