package producer

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// protectedInsertSQL is a pure string/arg builder -- these tests pin its
// shape. The row-comparison it emits is proven live against a real winner
// contest in the CompactionRank read/retention chunk, not here.

func TestProtectedInsertSQLUnkeyedDefaultRank(t *testing.T) {
	key := uuid.New()
	sql, args := protectedInsertSQL(1, key, "payload", ProduceOptions{RoutingKey: "r"})

	// all-default traffic: args shape is unchanged from before CompactionRank
	// existed -- unkeyed inserts never bind it, the column default (0) applies.
	want := []any{key, "payload", "r"}
	assertArgs(t, args, want)

	if strings.Contains(sql, "compaction_head") {
		t.Fatal("unkeyed insert must not touch compaction_head")
	}
	if strings.Contains(sql, "compaction_rank") || strings.Contains(sql, "compaction_key") {
		t.Fatalf("unkeyed insert should rely on the column defaults, not name them, got:\n%s", sql)
	}
}

func TestProtectedInsertSQLUnkeyedIgnoresCallerRank(t *testing.T) {
	// no CompactionKey means no contest to rank -- a caller-set rank here must
	// be silently dropped, not bound and stored as-given.
	key := uuid.New()
	sql, args := protectedInsertSQL(1, key, "payload", ProduceOptions{RoutingKey: "r", CompactionRank: 99})

	want := []any{key, "payload", "r"}
	assertArgs(t, args, want)

	if strings.Contains(sql, "$4") {
		t.Fatalf("unkeyed insert must not bind the caller's rank, got a 4th placeholder:\n%s", sql)
	}
}

func TestProtectedInsertSQLKeyedDefaultRank(t *testing.T) {
	key := uuid.New()
	sql, args := protectedInsertSQL(7, key, "payload", ProduceOptions{RoutingKey: "r", CompactionKey: "user:1"})

	want := []any{key, "payload", "r", "user:1", int64(7), int64(0)}
	assertArgs(t, args, want)

	if !strings.Contains(sql, "(compaction_head.compaction_rank, compaction_head.head_id) < (EXCLUDED.compaction_rank, EXCLUDED.head_id)") {
		t.Fatalf("keyed insert missing the (rank, id) row comparison, got:\n%s", sql)
	}
	// same-rank traffic (the all-default case) must fall through to today's
	// id-alone comparison -- true by construction since rank ties make the
	// tuple compare degenerate to comparing head_id alone.
}

func TestProtectedInsertSQLNegativeRank(t *testing.T) {
	// the bridge's exact use: a backfill write pinned below live traffic's
	// default 0, so it can never win a fresh key's contest.
	key := uuid.New()
	_, args := protectedInsertSQL(1, key, "payload", ProduceOptions{CompactionKey: "k", CompactionRank: -1})

	want := []any{key, "payload", "", "k", int64(1), int64(-1)}
	assertArgs(t, args, want)
}

func TestProtectedInsertSQLHigherRankArg(t *testing.T) {
	// a pinning write: rank carried through untouched, regardless of message id
	// (id isn't a producer-side concept at all -- Postgres assigns it).
	key := uuid.New()
	_, args := protectedInsertSQL(1, key, "payload", ProduceOptions{CompactionKey: "k", CompactionRank: 100})

	want := []any{key, "payload", "", "k", int64(1), int64(100)}
	assertArgs(t, args, want)
}

func TestProtectedInsertSQLReplaceOnChange(t *testing.T) {
	// the change guard swaps the plain compare for the head-row subquery; no age
	// term means no extra arg.
	key := uuid.New()
	sql, args := protectedInsertSQL(1, key, "payload", ProduceOptions{CompactionKey: "k", ReplaceOnChange: true})

	assertArgs(t, args, []any{key, "payload", "", "k", int64(1), int64(0)})

	if !strings.Contains(sql, "head.payload IS DISTINCT FROM $2::jsonb") {
		t.Fatalf("missing payload-change guard, got:\n%s", sql)
	}
	if strings.Contains(sql, "make_interval") {
		t.Fatalf("no age guard set, make_interval should be absent, got:\n%s", sql)
	}
}

func TestProtectedInsertSQLReplaceOlderThan(t *testing.T) {
	// the age guard binds its interval (seconds) as $7; no change guard means no
	// payload compare.
	key := uuid.New()
	sql, args := protectedInsertSQL(1, key, "payload", ProduceOptions{CompactionKey: "k", ReplaceOlderThan: 4 * time.Hour})

	assertArgs(t, args, []any{key, "payload", "", "k", int64(1), int64(0), (4 * time.Hour).Seconds()})

	if !strings.Contains(sql, "head.created_at < now() - make_interval(secs => $7)") {
		t.Fatalf("missing age guard bound to $7, got:\n%s", sql)
	}
	if strings.Contains(sql, "IS DISTINCT FROM") {
		t.Fatalf("no change guard set, payload compare should be absent, got:\n%s", sql)
	}
}

func TestProtectedInsertSQLReplaceBothOrGuards(t *testing.T) {
	// both guards -> the head is stale on change OR age, so the write wins on
	// either, and the plain compaction_head compare gives way to the head-row join.
	key := uuid.New()
	sql, args := protectedInsertSQL(1, key, "payload", ProduceOptions{CompactionKey: "k", ReplaceOnChange: true, ReplaceOlderThan: time.Hour})

	assertArgs(t, args, []any{key, "payload", "", "k", int64(1), int64(0), time.Hour.Seconds()})

	if !strings.Contains(sql, "head.payload IS DISTINCT FROM $2::jsonb") || !strings.Contains(sql, "make_interval(secs => $7)") {
		t.Fatalf("both guards should appear, got:\n%s", sql)
	}
	if strings.Contains(sql, "(compaction_head.compaction_rank, compaction_head.head_id) < (EXCLUDED.compaction_rank, EXCLUDED.head_id)") {
		t.Fatalf("guarded insert should weigh the head's own row, not compaction_head, got:\n%s", sql)
	}
	if !strings.Contains(sql, "WHERE head.id = compaction_head.head_id") {
		t.Fatalf("guarded insert should join the head's message_log row, got:\n%s", sql)
	}
}

func assertArgs(t *testing.T, got, want []any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %v, want %v (full: got=%+v want=%+v)", i, got[i], want[i], got, want)
		}
	}
}
