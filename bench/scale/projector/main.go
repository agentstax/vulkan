// Command projector materializes deliveries rows for designs 2 (NOTIFY) and 3
// (polling). It owns a shard of consumer groups, advances a frontier over the
// events log in fixed offset windows, and bulk-inserts one delivery row per
// (group, event) via a single set-based INSERT..SELECT per window. It prints a
// per-second throughput line (rows materialized/s) for the harness to parse.
//
// Modes:
//   -mode=poll   : sleep -poll-ms between catch-up passes
//   -mode=listen : LISTEN events_appended; wake on a (coalesced) notify, then
//                  catch up fully. The producer trigger emits one notify per
//                  INSERT statement (never per row).
//
// Sharding: instance i of n handles groups where
//   ((hashtextextended(group,0) % n)+n)%n = i
// Run n instances (shard 0..n-1, nshards n) for parallel materialization.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
)

func dsn() string {
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		get("PGUSER", "bench"), get("PGPASSWORD", "bench"),
		get("PGHOST", "localhost"), get("PGPORT", "5433"), get("PGDATABASE", "bench"))
}

func main() {
	mode := flag.String("mode", "poll", "poll|listen")
	batch := flag.Int64("batch", 5000, "offset-window size per materialize pass")
	pollMs := flag.Int("poll-ms", 50, "poll sleep when idle (poll mode)")
	shard := flag.Int("shard", 0, "this instance's shard index")
	nshards := flag.Int("nshards", 1, "total shards")
	dur := flag.Int("dur", 0, "run seconds (0 = until SIGINT/SIGTERM)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if *dur > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*dur)*time.Second)
		defer cancel()
	}

	work, err := pgx.Connect(ctx, dsn())
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect work:", err)
		os.Exit(1)
	}
	defer work.Close(context.Background())

	// shard predicate over cursors.consumer_group
	shardPred := fmt.Sprintf("(((hashtextextended(consumer_group,0) %% %d)+%d) %% %d) = %d",
		*nshards, *nshards, *nshards, *shard)

	var frontier int64
	if err := work.QueryRow(ctx,
		"SELECT COALESCE(min(projected),0) FROM cursors WHERE "+shardPred).Scan(&frontier); err != nil {
		fmt.Fprintln(os.Stderr, "init frontier:", err)
		os.Exit(1)
	}

	insertSQL := "INSERT INTO deliveries(consumer_group,\"offset\",state) " +
		"SELECT c.consumer_group, e.\"offset\", 'ready' " +
		"FROM cursors c CROSS JOIN events e " +
		"WHERE " + shardPred + " AND e.\"offset\" > $1 AND e.\"offset\" <= $2 " +
		"ON CONFLICT DO NOTHING"
	advanceSQL := "UPDATE cursors SET projected = $1 WHERE " + shardPred + " AND projected < $1"

	// optional LISTEN connection
	var lconn *pgx.Conn
	if *mode == "listen" {
		lconn, err = pgx.Connect(ctx, dsn())
		if err != nil {
			fmt.Fprintln(os.Stderr, "connect listen:", err)
			os.Exit(1)
		}
		defer lconn.Close(context.Background())
		if _, err := lconn.Exec(ctx, "LISTEN events_appended"); err != nil {
			fmt.Fprintln(os.Stderr, "LISTEN:", err)
			os.Exit(1)
		}
	}

	start := time.Now()
	var rowsTotal int64
	lastLog := start
	var lastRows int64
	report := func() {
		now := time.Now()
		dt := now.Sub(lastLog).Seconds()
		if dt <= 0 {
			return
		}
		fmt.Printf("proj shard=%d t=%.1f rows_total=%d rate=%.0f/s\n",
			*shard, now.Sub(start).Seconds(), rowsTotal, float64(rowsTotal-lastRows)/dt)
		lastLog = now
		lastRows = rowsTotal
	}

	// one catch-up pass: materialize windows until frontier == head
	catchUp := func() error {
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var head int64
			if err := work.QueryRow(ctx, "SELECT COALESCE(max(\"offset\"),0) FROM events").Scan(&head); err != nil {
				return err
			}
			if frontier >= head {
				return nil
			}
			hi := frontier + *batch
			if hi > head {
				hi = head
			}
			tag, err := work.Exec(ctx, insertSQL, frontier, hi)
			if err != nil {
				return err
			}
			rowsTotal += tag.RowsAffected()
			if _, err := work.Exec(ctx, advanceSQL, hi); err != nil {
				return err
			}
			frontier = hi
			if time.Since(lastLog) >= time.Second {
				report()
			}
		}
	}

	for ctx.Err() == nil {
		if err := catchUp(); err != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintln(os.Stderr, "catchUp:", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if time.Since(lastLog) >= time.Second {
			report()
		}
		if *mode == "listen" {
			wctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			_, _ = lconn.WaitForNotification(wctx)
			cancel()
		} else {
			time.Sleep(time.Duration(*pollMs) * time.Millisecond)
		}
	}
	report()
	fmt.Printf("proj shard=%d DONE elapsed=%.1f rows_total=%d\n", *shard, time.Since(start).Seconds(), rowsTotal)
}
