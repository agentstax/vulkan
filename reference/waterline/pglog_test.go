package waterline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These are integration tests against a throwaway Postgres 18 on :5433 (see the
// reference README for the docker run). They port the SQL audit
// (drivers/audit_waterline_v2.sh, scenarios T1-T6) to Go and re-assert the six
// load-bearing invariants R1-R6, plus the routing / partition / compaction
// features. Set WATERLINE_DSN to point elsewhere; tests SKIP if the DB is down.

var testLog *PgLog

func TestMain(m *testing.M) {
	dsn := os.Getenv("WATERLINE_DSN")
	if dsn == "" {
		dsn = "postgres://bench:bench@localhost:5433/bench"
	}
	ctx := context.Background()
	// The test harness TRUNCATEs between every test under a shared pool. Use the
	// simple "exec" mode (no cached server-prepared statements) so a connection
	// can never carry a plan that desyncs across the schema churn — a robustness
	// choice for the test pool only; New() keeps the fast cached default that the
	// recorded benchmark numbers were taken with.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: bad dsn %s: %v\n", dsn, err)
		os.Exit(0)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil || pool.Ping(ctx) != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot reach %s: %v\n", dsn, err)
		os.Exit(0)
	}
	l := &PgLog{Pool: pool}
	testLog = l
	// Migrate ONCE here (fresh pool, nothing cached). Per-test reset() then
	// TRUNCATEs rather than re-running the DDL: dropping/recreating tables under a
	// shared, statement-caching pool would leave pooled connections with cached
	// plans bound to the dropped tables' OIDs (a flaky "column does not exist").
	if err := l.Migrate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	l.Close()
	os.Exit(code)
}

// reset truncates all tables to a clean state (RESTART IDENTITY so offsets start
// at 1 again), without DDL — see TestMain for why.
func reset(t *testing.T) *PgLog {
	t.Helper()
	mustExec(t, `TRUNCATE events, cursors, leases, deliveries, bindings, processed RESTART IDENTITY`)
	return testLog
}

func mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := testLog.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func scalarInt(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := testLog.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("scalar %q: %v", sql, err)
	}
	return n
}

// seed appends n trivial events and returns the head.
func seed(t *testing.T, l *PgLog, n int) int64 {
	t.Helper()
	head, err := l.AppendBatch(context.Background(), n, []byte(`{"x":1}`), nil, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return head
}

// recordProcessed bumps the processed ledger (gap/double-process detector).
func recordProcessed(l *PgLog, group string, off int64) {
	_, _ = l.Pool.Exec(context.Background(),
		`INSERT INTO processed(consumer_group,"offset",times) VALUES ($1,$2,1)
		 ON CONFLICT (consumer_group,"offset") DO UPDATE SET times=processed.times+1`,
		group, off)
}

// drainHappy runs the happy-path worker loop (reclaim-then-claim) until caught
// up, calling process per event. process returns an error to park an exception.
func drainHappy(t *testing.T, l *PgLog, group string, lane, batch int, process func(Event) error) {
	t.Helper()
	ctx := context.Background()
	lease := 30 * time.Second
	for {
		r, evs, reclaimed, err := l.Reclaim(ctx, group, lease, 0)
		if err != nil {
			t.Fatalf("reclaim: %v", err)
		}
		if !reclaimed {
			r, evs, err = l.Claim(ctx, group, lane, batch, lease)
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
		}
		if r.Empty() {
			return
		}
		var exc []Exception
		for _, e := range evs {
			if perr := process(e); perr != nil {
				exc = append(exc, Exception{Offset: e.Offset, Err: perr.Error()})
			}
		}
		if err := l.Commit(ctx, r, exc); err != nil && !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("commit: %v", err)
		}
		if _, err := l.Advance(ctx, group, r.Lane); err != nil {
			rows := scalarInt(t, `SELECT count(*) FROM cursors WHERE consumer_group=$1 AND lane=$2`, group, r.Lane)
			all := scalarInt(t, `SELECT count(*) FROM cursors`)
			t.Fatalf("advance: %v | r=%+v | cursor(group,lane) rows=%d total cursors=%d", err, r, rows, all)
		}
	}
}

// ---------------------------------------------------------------------------
// Terminal invariants — the audit's "done == head, no gap/dup, 0 dangling
// leases, waterline == head" for the claim-from-log + waterline structure.
// ---------------------------------------------------------------------------

func TestHappyPathTerminalInvariants(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, n = "g", 5000
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, n)

	drainHappy(t, l, group, 0, 500, func(e Event) error {
		recordProcessed(l, group, e.Offset)
		return nil
	})

	if got := scalarInt(t, `SELECT count(*) FROM processed WHERE consumer_group=$1`, group); got != n {
		t.Errorf("processed count = %d, want %d (gap)", got, n)
	}
	if dup := scalarInt(t, `SELECT count(*) FROM processed WHERE consumer_group=$1 AND times>1`, group); dup != 0 {
		t.Errorf("double-processed %d offsets", dup)
	}
	if leases := scalarInt(t, `SELECT count(*) FROM leases WHERE consumer_group=$1`, group); leases != 0 {
		t.Errorf("dangling leases = %d", leases)
	}
	wm, _ := l.Watermark(ctx, group)
	if wm != n {
		t.Errorf("watermark = %d, want %d", wm, n)
	}
	if up, _ := l.CaughtUp(ctx, group); !up {
		t.Error("not caught up")
	}
}

// T4 (R6): all-failing offsets reach dead and the waterline advances past them.
func TestDeadUnblocksWaterline(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, n, maxAtt = "m", 200, 3
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, n)

	// Happy path: everything fails first pass -> parked as exceptions.
	drainHappy(t, l, group, 0, 1000, func(e Event) error { return errors.New("boom") })
	if open := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND state='ready'`, group); open != n {
		t.Fatalf("parked %d, want %d", open, n)
	}

	// Exception drain: every reprocess fails; after maxAtt attempts -> dead.
	for round := 0; round < maxAtt+2; round++ {
		mustExec(t, `UPDATE deliveries SET available_at=now() WHERE consumer_group=$1 AND state='ready'`, group)
		ds, evs, err := l.ClaimExceptions(ctx, group, 100000, maxAtt, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(ds) == 0 {
			break
		}
		for i := range ds {
			_ = evs
			if err := l.Nack(ctx, maxAtt, &ds[i], errors.New("still boom")); err != nil {
				t.Fatalf("nack: %v", err)
			}
		}
	}

	dead := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND state='dead'`, group)
	openLeft := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND state IN ('ready','inflight')`, group)
	if dead != n {
		t.Errorf("dead = %d, want %d", dead, n)
	}
	if openLeft != 0 {
		t.Errorf("still open = %d, want 0", openLeft)
	}
	committed, _ := l.Advance(ctx, group, 0)
	if committed != n {
		t.Errorf("committed = %d, want %d (dead must unblock)", committed, n)
	}
}

// T1 (R3): a stale worker's Commit after its lease was reclaimed must be a
// no-op (ErrLeaseLost) and inject NO phantom exception row.
func TestStaleCommitNoPhantom(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "s"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 100)

	r, _, err := l.Claim(ctx, group, 0, 10, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group=$1`, group)
	r2, _, ok, err := l.Reclaim(ctx, group, 30*time.Second, 0)
	if err != nil || !ok {
		t.Fatalf("reclaim ok=%v err=%v", ok, err)
	}
	if err := l.Commit(ctx, r2, nil); err != nil { // reclaimer commits cleanly, frees lease
		t.Fatalf("reclaimer commit: %v", err)
	}
	// original stale worker tries to commit with an exception:
	err = l.Commit(ctx, r, []Exception{{Offset: 5, Err: "stale"}})
	if !errors.Is(err, ErrLeaseLost) {
		t.Errorf("stale commit err = %v, want ErrLeaseLost", err)
	}
	if rows := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1`, group); rows != 0 {
		t.Errorf("phantom deliveries = %d, want 0", rows)
	}
}

// T6 (R5): a second reclaimer must SKIP a lease another session holds locked.
func TestReclaimSkipLocked(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "k"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 100)
	if _, _, err := l.Claim(ctx, group, 0, 10, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group=$1`, group)

	// Session A holds a row lock on the only expired lease.
	conn, err := l.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM leases WHERE consumer_group=$1 AND lease_until<now() FOR UPDATE`, group); err != nil {
		t.Fatal(err)
	}

	// A concurrent Reclaim from the pool must skip the locked lease.
	_, _, ok, err := l.Reclaim(ctx, group, 30*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("reclaim grabbed a lease that was locked by another session")
	}
}

// T2 (R1): sharded Claim assigns DISJOINT contiguous blocks (the draft bug had
// all lanes return the same (0,batch] range).
func TestShardedClaimDisjoint(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, n, k = "q", 1000, 4
	seed(t, l, n)
	if _, err := l.InitLanes(ctx, group, k); err != nil {
		t.Fatal(err)
	}
	type rng struct{ lo, hi int64 }
	got := make([]rng, k)
	for ln := 0; ln < k; ln++ {
		r, _, err := l.Claim(ctx, group, ln, 200, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		got[ln] = rng{r.Lo, r.Hi}
	}
	// Each lane's lo must equal its block floor (n*ln/k): 0, 250, 500, 750.
	for ln := 0; ln < k; ln++ {
		wantLo := int64(n) * int64(ln) / int64(k)
		if got[ln].lo != wantLo {
			t.Errorf("lane %d lo = %d, want %d (lanes not disjoint => dup/gap)", ln, got[ln].lo, wantLo)
		}
	}
	// Ranges must not overlap.
	sort.Slice(got, func(i, j int) bool { return got[i].lo < got[j].lo })
	for i := 1; i < k; i++ {
		if got[i].lo < got[i-1].hi {
			t.Errorf("lane ranges overlap: %v and %v", got[i-1], got[i])
		}
	}
}

// T3 (R2): Advance's exception blocker is lane-scoped — a lane-1 exception must
// not floor lane-0's waterline.
func TestLaneScopedAdvance(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, n, k = "w", 1000, 2
	seed(t, l, n)
	if _, err := l.InitLanes(ctx, group, k); err != nil {
		t.Fatal(err)
	}
	// Lane 0: claim (0,400] and commit cleanly so claimed=400, no leases, no exceptions.
	r, _, err := l.Claim(ctx, group, 0, 400, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Commit(ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	// A lane-1 exception at offset 600 must NOT block lane 0.
	mustExec(t, `INSERT INTO deliveries(consumer_group,lane,"offset",state) VALUES ($1,1,600,'ready')`, group)
	committed, err := l.Advance(ctx, group, 0)
	if err != nil {
		t.Fatal(err)
	}
	if committed != 400 {
		t.Errorf("lane-0 committed = %d, want 400 (lane-1 exception leaked across lanes)", committed)
	}
}

// Crash safety (d5): a range claimed-but-not-committed is reclaimed and fully
// reprocessed (at-least-once).
func TestCrashReclaimReprocess(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "c"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 50)
	// "crash": claim a range, process nothing, never commit.
	if _, _, err := l.Claim(ctx, group, 0, 50, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group=$1`, group)
	// recover: reclaim-then-claim drains the orphaned range.
	drainHappy(t, l, group, 0, 50, func(e Event) error { recordProcessed(l, group, e.Offset); return nil })
	if got := scalarInt(t, `SELECT count(*) FROM processed WHERE consumer_group=$1`, group); got != 50 {
		t.Errorf("processed = %d, want 50 (crashed range not recovered)", got)
	}
	wm, _ := l.Watermark(ctx, group)
	if wm != 50 {
		t.Errorf("watermark = %d, want 50", wm)
	}
}

// ---------------------------------------------------------------------------
// Phase 7 — Routing
// ---------------------------------------------------------------------------

func TestRoutingTopicAndHeader(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	app := func(rk, headers string) {
		s := AppendSpec{RoutingKey: &rk, Headers: headers}
		if _, err := l.Append(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	app("orders.eu.created", `{"region":"eu"}`)   // off 1
	app("orders.us.created", `{"region":"us"}`)   // off 2
	app("orders.us.shipped", `{"region":"us"}`)   // off 3
	app("payments.eu.settled", `{"region":"eu"}`) // off 4

	for _, g := range []string{"A", "B", "C"} {
		if err := l.EnsureGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.BindTopic(ctx, "A", "orders.*.created"); err != nil { // -> off 1,2
		t.Fatal(err)
	}
	if err := l.BindTopic(ctx, "B", "orders.us.>"); err != nil { // -> off 2,3
		t.Fatal(err)
	}
	if err := l.BindHeader(ctx, "C", `{"region":"eu"}`); err != nil { // -> off 1,4
		t.Fatal(err)
	}

	collect := func(group string) []int64 {
		var offs []int64
		drainHappy(t, l, group, 0, 100, func(e Event) error { offs = append(offs, e.Offset); return nil })
		sort.Slice(offs, func(i, j int) bool { return offs[i] < offs[j] })
		return offs
	}
	if got := collect("A"); !eqOffs(got, []int64{1, 2}) {
		t.Errorf("A got %v, want [1 2]", got)
	}
	if got := collect("B"); !eqOffs(got, []int64{2, 3}) {
		t.Errorf("B got %v, want [2 3]", got)
	}
	if got := collect("C"); !eqOffs(got, []int64{1, 4}) {
		t.Errorf("C got %v, want [1 4]", got)
	}
	// Cursor still advances over the WHOLE log even though only a subset matched.
	for _, g := range []string{"A", "B", "C"} {
		if wm, _ := l.Watermark(ctx, g); wm != 4 {
			t.Errorf("group %s watermark = %d, want 4 (cursor must advance over all offsets)", g, wm)
		}
	}
}

func eqOffs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Phase 8 — FIFO partitions
// ---------------------------------------------------------------------------

// Keyed messages process in per-key order under many concurrent workers; unkeyed
// parallelize. Proven by recording the processing order per key and asserting it
// is strictly ascending by offset.
func TestPartitionFIFOConcurrent(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "p"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	keys := []string{"A", "B", "C"}
	const perKey = 12
	for i := 0; i < perKey; i++ {
		for _, k := range keys {
			kk := k
			if _, err := l.Append(ctx, AppendSpec{PartitionKey: &kk, Payload: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := l.Append(ctx, AppendSpec{Payload: []byte(`{}`)}); err != nil { // unkeyed
			t.Fatal(err)
		}
	}
	total, err := l.Materialize(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(perKey*(len(keys)+1)) {
		t.Fatalf("materialized %d, want %d", total, perKey*(len(keys)+1))
	}

	var mu sync.Mutex
	order := map[string][]int64{}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idle := 0
			for idle < 3 {
				ds, _, err := l.ClaimPartitioned(ctx, group, 4, 5, 30*time.Second)
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if len(ds) == 0 {
					idle++
					time.Sleep(5 * time.Millisecond)
					continue
				}
				idle = 0
				for i := range ds {
					if ds[i].PartitionKey != nil {
						mu.Lock()
						order[*ds[i].PartitionKey] = append(order[*ds[i].PartitionKey], ds[i].Offset)
						mu.Unlock()
					}
					if err := l.Ack(ctx, &ds[i]); err != nil {
						t.Errorf("ack: %v", err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()

	for _, k := range keys {
		offs := order[k]
		if len(offs) != perKey {
			t.Errorf("key %s processed %d, want %d", k, len(offs), perKey)
		}
		for i := 1; i < len(offs); i++ {
			if offs[i] <= offs[i-1] {
				t.Errorf("key %s out of order: %v", k, offs)
				break
			}
		}
	}
	if remaining := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1`, group); remaining != 0 {
		t.Errorf("deliveries left = %d, want 0", remaining)
	}
	// FIFO uses the same lazy waterline roll-up: once drained, Advance must carry
	// committed to head so Watermark/CaughtUp/CompactSafe are correct ([11]).
	if _, err := l.Advance(ctx, group, 0); err != nil {
		t.Fatal(err)
	}
	head, _ := l.Head(ctx)
	if wm, _ := l.Watermark(ctx, group); wm != head {
		t.Errorf("FIFO watermark = %d, want head %d", wm, head)
	}
}

// FIFO survives a retry: a key's failed-and-backing-off head blocks its later
// offsets; a dead head stops blocking.
func TestPartitionFIFOThroughRetry(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "r"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	key := "X"
	var offs []int64
	for i := 0; i < 3; i++ {
		off, err := l.Append(ctx, AppendSpec{PartitionKey: &key, Payload: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		offs = append(offs, off)
	}
	if _, err := l.Materialize(ctx, group); err != nil {
		t.Fatal(err)
	}

	claimOne := func() (Delivery, bool) {
		ds, _, err := l.ClaimPartitioned(ctx, group, 10, 5, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(ds) == 0 {
			return Delivery{}, false
		}
		if len(ds) != 1 {
			t.Fatalf("claimed %d for one key, want 1 (FIFO violated)", len(ds))
		}
		return ds[0], true
	}

	d, ok := claimOne()
	if !ok || d.Offset != offs[0] {
		t.Fatalf("first claim = %v ok=%v, want offset %d", d.Offset, ok, offs[0])
	}
	// Nack the head -> ready with backoff; later offsets must stay blocked.
	if err := l.Nack(ctx, 5, &d, errors.New("retry")); err != nil {
		t.Fatal(err)
	}
	if _, ok := claimOne(); ok {
		t.Error("claimed an offset while the key head was backing off (FIFO-through-retry violated)")
	}
	// Make the head available; it must come back (not a later offset).
	mustExec(t, `UPDATE deliveries SET available_at=now() WHERE consumer_group=$1 AND "offset"=$2`, group, offs[0])
	d, ok = claimOne()
	if !ok || d.Offset != offs[0] {
		t.Fatalf("retry claim = %d ok=%v, want head %d", d.Offset, ok, offs[0])
	}
	// Dead-letter the head -> it stops blocking; next offset becomes eligible.
	if err := l.DeadLetter(ctx, &d, errors.New("terminal")); err != nil {
		t.Fatal(err)
	}
	d, ok = claimOne()
	if !ok || d.Offset != offs[1] {
		t.Fatalf("after dead head, claim = %d ok=%v, want %d", d.Offset, ok, offs[1])
	}
}

// ---------------------------------------------------------------------------
// Phase 9 — Log compaction (compacted topics)
// ---------------------------------------------------------------------------

func TestCompactionKeepLatest(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	k1, k2 := "K1", "K2"
	app := func(key *string, payload []byte) int64 {
		off, err := l.Append(ctx, AppendSpec{PartitionKey: key, Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		return off
	}
	app(&k1, []byte(`{"v":1}`))
	app(&k1, []byte(`{"v":2}`))
	k1latest := app(&k1, []byte(`{"v":3}`))
	app(&k2, []byte(`{"v":1}`))
	app(&k2, nil) // tombstone -> K2 removed entirely
	unkeyed := app(nil, []byte(`{"u":1}`))

	head, _ := l.Head(ctx)
	res, err := l.Compact(ctx, "", head) // HeadFloor = true compacted-topic semantics
	if err != nil {
		t.Fatal(err)
	}
	if res.Superseded != 3 { // 2 old K1 + 1 old K2
		t.Errorf("superseded = %d, want 3", res.Superseded)
	}
	if res.Tombstoned != 1 {
		t.Errorf("tombstoned = %d, want 1", res.Tombstoned)
	}
	if c := scalarInt(t, `SELECT count(*) FROM events WHERE partition_key='K1'`); c != 1 {
		t.Errorf("K1 rows = %d, want 1", c)
	}
	if c := scalarInt(t, `SELECT "offset" FROM events WHERE partition_key='K1'`); c != k1latest {
		t.Errorf("surviving K1 offset = %d, want %d (latest)", c, k1latest)
	}
	if c := scalarInt(t, `SELECT count(*) FROM events WHERE partition_key='K2'`); c != 0 {
		t.Errorf("K2 rows = %d, want 0 (tombstoned)", c)
	}
	if c := scalarInt(t, `SELECT count(*) FROM events WHERE partition_key IS NULL`); c != 1 {
		t.Errorf("unkeyed rows = %d, want 1 (never compacted)", c)
	}
	_ = unkeyed
}

func TestCompactionWatermarkSafe(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	key := "K"
	app := func(payload []byte) int64 {
		off, err := l.Append(ctx, AppendSpec{PartitionKey: &key, Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		return off
	}
	app([]byte(`{"v":1}`))
	app([]byte(`{"v":2}`))
	app([]byte(`{"v":3}`))

	// A group that has consumed NOTHING (committed=0): the watermark-safe floor is
	// 0, so no version may be compacted — even a wants-every-version consumer is safe.
	if err := l.EnsureGroup(ctx, "slow"); err != nil {
		t.Fatal(err)
	}
	res, err := l.CompactSafe(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Superseded != 0 {
		t.Errorf("superseded = %d, want 0 (floor=0 must protect all unconsumed versions)", res.Superseded)
	}
	if c := scalarInt(t, `SELECT count(*) FROM events WHERE partition_key='K'`); c != 3 {
		t.Errorf("K rows = %d, want 3 (all protected below watermark)", c)
	}

	// Once the group has consumed through the head, the floor rises and the old
	// versions become compactable.
	head, _ := l.Head(ctx)
	mustExec(t, `UPDATE cursors SET committed=$2, claimed=$2 WHERE consumer_group=$1`, "slow", head)
	res, err = l.CompactSafe(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Superseded != 2 {
		t.Errorf("superseded = %d, want 2 (floor=head now permits compaction)", res.Superseded)
	}
	if c := scalarInt(t, `SELECT count(*) FROM events WHERE partition_key='K'`); c != 1 {
		t.Errorf("K rows = %d, want 1 after floor rose", c)
	}
}

// Regression for the review finding: compaction must NOT delete an event body
// that a deliveries row still references. A dead/DLQ entry for a key that was
// later superseded is the natural case — committed rises past dead rows, so the
// watermark floor alone would let the old body be compacted, orphaning the DLQ.
func TestCompactionPreservesDLQ(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "g"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	key := "K"
	v1, err := l.Append(ctx, AppendSpec{PartitionKey: &key, Payload: []byte(`{"v":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(ctx, AppendSpec{PartitionKey: &key, Payload: []byte(`{"v":2}`)}); err != nil {
		t.Fatal(err)
	}
	// v1 dead-letters (a DLQ entry remains at offset v1); the group then drains to
	// head so committed rises PAST the dead row.
	mustExec(t, `INSERT INTO deliveries(consumer_group,lane,"offset",partition_key,state,last_error)
	             VALUES ($1,0,$2,$3,'dead','poison')`, group, v1, key)
	head, _ := l.Head(ctx)
	mustExec(t, `UPDATE cursors SET committed=$2, claimed=$2 WHERE consumer_group=$1`, group, head)

	if floor, _ := l.CompactFloor(ctx); floor < v1 {
		t.Fatalf("precondition: floor=%d should be >= dead offset %d for this test to be meaningful", floor, v1)
	}
	if _, err := l.CompactSafe(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if c := scalarInt(t, `SELECT count(*) FROM events WHERE "offset"=$1`, v1); c != 1 {
		t.Errorf("v1 body deleted (%d rows) — DLQ entry at offset %d orphaned", c, v1)
	}
	if c := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND "offset"=$2 AND state='dead'`, group, v1); c != 1 {
		t.Errorf("DLQ entry missing")
	}
}

// ---------------------------------------------------------------------------
// Regression tests for the adversarial-review findings
// ---------------------------------------------------------------------------

// [1][2] InitLanes guards: empty/undersized log and re-init are rejected.
func TestInitLanesGuards(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	if _, err := l.InitLanes(ctx, "g", 4); !errors.Is(err, ErrLogTooSmall) {
		t.Errorf("empty-log InitLanes err = %v, want ErrLogTooSmall", err)
	}
	seed(t, l, 2)
	if _, err := l.InitLanes(ctx, "g", 4); !errors.Is(err, ErrLogTooSmall) {
		t.Errorf("head<k InitLanes err = %v, want ErrLogTooSmall", err)
	}
	if _, err := l.InitLanes(ctx, "g", 2); err != nil {
		t.Fatalf("valid InitLanes: %v", err)
	}
	if _, err := l.InitLanes(ctx, "g", 2); !errors.Is(err, ErrLanesExist) {
		t.Errorf("re-init err = %v, want ErrLanesExist", err)
	}
}

// [1] A sharded group must drain all the way to CaughtUp (no empty lane pins Watermark).
func TestShardedDrainsToCaughtUp(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, n, k = "sg", 103, 4 // not divisible by k, to exercise uneven blocks
	seed(t, l, n)
	if _, err := l.InitLanes(ctx, group, k); err != nil {
		t.Fatal(err)
	}
	for lane := 0; lane < k; lane++ {
		for {
			r, _, err := l.Claim(ctx, group, lane, 10, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if r.Empty() {
				break
			}
			if err := l.Commit(ctx, r, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Advance(ctx, group, lane); err != nil {
				t.Fatal(err)
			}
		}
	}
	if up, _ := l.CaughtUp(ctx, group); !up {
		wm, _ := l.Watermark(ctx, group)
		t.Errorf("sharded group not caught up; watermark=%d head=%d", wm, n)
	}
}

// [6][16] A crashed exception worker's inflight row is reclaimed (folded into the claim).
func TestExceptionCrashReclaim(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "excrash"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 5)
	drainHappy(t, l, group, 0, 10, func(e Event) error { return errors.New("boom") }) // park 5
	mustExec(t, `UPDATE deliveries SET available_at=now() WHERE consumer_group=$1`, group)
	ds, _, err := l.ClaimExceptions(ctx, group, 100, 5, 1*time.Millisecond) // claim, short lease
	if err != nil || len(ds) != 5 {
		t.Fatalf("claim: len=%d err=%v", len(ds), err)
	}
	time.Sleep(20 * time.Millisecond) // "crash": never ack/nack; lease expires
	ds2, _, err := l.ClaimExceptions(ctx, group, 100, 5, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds2) != 5 {
		t.Errorf("reclaimed %d expired-inflight, want 5 (crashed exception worker not recovered)", len(ds2))
	}
}

// Crash-loop backstop: a message that hits maxAttempts while expired-inflight is
// reaped to dead WITHOUT user code (the worker keeps crashing before nack).
func TestExceptionCrashLoopReapedToDead(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, maxAtt = "crashloop", 3
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 1)
	drainHappy(t, l, group, 0, 10, func(e Event) error { return errors.New("boom") })
	mustExec(t, `UPDATE deliveries SET available_at=now() WHERE consumer_group=$1`, group)
	for i := 0; i < maxAtt+3; i++ {
		if _, _, err := l.ClaimExceptions(ctx, group, 10, maxAtt, 1*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		time.Sleep(8 * time.Millisecond)
	}
	if dead := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND state='dead'`, group); dead != 1 {
		t.Errorf("dead=%d want 1 (crash-loop not reaped to dead)", dead)
	}
}

// [10] A crashed FIFO worker's inflight head is reclaimed; a LIVE one still blocks.
func TestPartitionCrashReclaim(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "pcrash"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	key := "K"
	for i := 0; i < 3; i++ {
		if _, err := l.Append(ctx, AppendSpec{PartitionKey: &key, Payload: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.Materialize(ctx, group); err != nil {
		t.Fatal(err)
	}
	ds, _, err := l.ClaimPartitioned(ctx, group, 10, 5, 30*time.Second)
	if err != nil || len(ds) != 1 {
		t.Fatalf("claim head: len=%d err=%v", len(ds), err)
	}
	head := ds[0].Offset
	if ds2, _, _ := l.ClaimPartitioned(ctx, group, 10, 5, 30*time.Second); len(ds2) != 0 {
		t.Errorf("claimed %d while head LIVE-inflight, want 0 (partition must stay blocked)", len(ds2))
	}
	mustExec(t, `UPDATE deliveries SET lease_until=now()-interval '1 min' WHERE consumer_group=$1 AND "offset"=$2`, group, head)
	ds3, _, _ := l.ClaimPartitioned(ctx, group, 10, 5, 30*time.Second)
	if len(ds3) != 1 || ds3[0].Offset != head {
		t.Errorf("reclaim got %+v, want head %d (crashed FIFO worker not recovered)", ds3, head)
	}
}

// [14][15] ClaimExceptions returns ds and evs ALIGNED by index (offset-paired).
func TestClaimExceptionsAligned(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group = "align"
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 60)
	drainHappy(t, l, group, 0, 100, func(e Event) error { return errors.New("x") }) // park 60
	mustExec(t, `UPDATE deliveries SET available_at=now() WHERE consumer_group=$1`, group)
	ds, evs, err := l.ClaimExceptions(ctx, group, 60, 5, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != len(evs) || len(ds) == 0 {
		t.Fatalf("len ds=%d evs=%d", len(ds), len(evs))
	}
	for i := range ds {
		if ds[i].Offset != evs[i].Offset {
			t.Errorf("misaligned at %d: delivery offset=%d but event offset=%d", i, ds[i].Offset, evs[i].Offset)
		}
	}
}

// [8] BindHeader with an empty containment object is rejected (it would match all).
func TestBindHeaderRejectsEmpty(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	if err := l.EnsureGroup(ctx, "g"); err != nil {
		t.Fatal(err)
	}
	if err := l.BindHeader(ctx, "g", "{}"); err == nil {
		t.Error("BindHeader({}) should be rejected")
	}
	if err := l.BindHeader(ctx, "g", "   "); err == nil {
		t.Error("BindHeader(blank) should be rejected")
	}
	if err := l.BindHeader(ctx, "g", `{"region":"eu"}`); err != nil {
		t.Errorf("BindHeader(valid) err = %v", err)
	}
}

// [5] A crash-looping (worker-crashing) batch is quarantined to the exception
// window after maxReclaims, so the waterline is no longer pinned by the lease.
func TestReclaimPoisonQuarantine(t *testing.T) {
	l := reset(t)
	ctx := context.Background()
	const group, maxReclaims = "poison", 3
	if err := l.EnsureGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	seed(t, l, 10)
	if _, _, err := l.Claim(ctx, group, 0, 10, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	quarantined := false
	for i := 0; i < maxReclaims+2; i++ {
		mustExec(t, `UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group=$1`, group)
		_, _, ok, err := l.Reclaim(ctx, group, 30*time.Second, maxReclaims)
		if err != nil {
			t.Fatal(err)
		}
		if !ok && scalarInt(t, `SELECT count(*) FROM leases WHERE consumer_group=$1`, group) == 0 {
			quarantined = true
			break
		}
	}
	if !quarantined {
		t.Fatal("poison range never quarantined")
	}
	if exc := scalarInt(t, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND state='ready'`, group); exc != 10 {
		t.Errorf("quarantined exceptions=%d, want 10", exc)
	}
}

var _ = pgx.ErrNoRows
