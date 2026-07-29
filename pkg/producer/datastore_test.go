package producer

import (
	"strings"
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/google/uuid"
)

// protectedInsertSQL is a pure string/arg builder -- these tests pin its
// shape. The row-comparison it emits is proven live against a real winner
// contest in the CompactionRank read/retention chunk, not here.

// noOptions is the trailing options arg for messages that request nothing --
// bound as a typed nil so the column lands NULL.
var noOptions *common.MessageOptions

func TestProtectedInsertSQLUnkeyedDefaultRank(t *testing.T) {
	key := uuid.New()
	sql, args := protectedInsertSQL(1, key, "payload", ProduceOptions{RoutingKey: "r"})

	want := []any{key, "payload", "r", noOptions}
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

	want := []any{key, "payload", "r", noOptions}
	assertArgs(t, args, want)

	if strings.Contains(sql, "$5") {
		t.Fatalf("unkeyed insert must not bind the caller's rank, got a 5th placeholder:\n%s", sql)
	}
}

func TestProtectedInsertSQLKeyedDefaultRank(t *testing.T) {
	key := uuid.New()
	sql, args := protectedInsertSQL(7, key, "payload", ProduceOptions{RoutingKey: "r", CompactionKey: "user:1"})

	want := []any{key, "payload", "r", "user:1", int64(7), int64(0), noOptions}
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

	want := []any{key, "payload", "", "k", int64(1), int64(-1), noOptions}
	assertArgs(t, args, want)
}

func TestProtectedInsertSQLHigherRankArg(t *testing.T) {
	// a pinning write: rank carried through untouched, regardless of message id
	// (id isn't a producer-side concept at all -- Postgres assigns it).
	key := uuid.New()
	_, args := protectedInsertSQL(1, key, "payload", ProduceOptions{CompactionKey: "k", CompactionRank: 100})

	want := []any{key, "payload", "", "k", int64(1), int64(100), noOptions}
	assertArgs(t, args, want)
}

func TestProtectedInsertSQLMessageOptionsBound(t *testing.T) {
	// a message that requests something binds a pointer (jsonb column); one
	// that requests nothing binds the typed nil above (NULL column) -- covered
	// by every other test here.
	key := uuid.New()
	opts := ProduceOptions{Message: &common.MessageOptions{WorkTimeout: time.Minute}}

	sql, args := protectedInsertSQL(1, key, "payload", opts)
	if !strings.Contains(sql, "options") {
		t.Fatalf("insert missing the options column, got:\n%s", sql)
	}
	bound, ok := args[len(args)-1].(*common.MessageOptions)
	if !ok || bound == nil {
		t.Fatalf("last arg = %#v, want *common.MessageOptions", args[len(args)-1])
	}
	if bound.WorkTimeout != time.Minute {
		t.Fatalf("bound options = %+v, want WorkTimeout %v", bound, time.Minute)
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
