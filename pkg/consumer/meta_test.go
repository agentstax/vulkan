package consumer

import (
	"context"
	"testing"
	"time"
)

func TestMetaFromContextBareContext(t *testing.T) {
	if _, ok := MetaFromContext(context.Background()); ok {
		t.Fatal("expected ok=false on a context with no meta stamped")
	}
}

func TestMetaFromContextRoundTrip(t *testing.T) {
	want := MessageMeta{Id: 42, RoutingKey: "orders.created", CompactionKey: "user:1", CompactionRank: 7, CreatedAt: time.Now()}
	ctx := withMeta(context.Background(), want)

	got, ok := MetaFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true after withMeta")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMessageRowToMessageMeta(t *testing.T) {
	row := MessageRow{Id: 1, RoutingKey: "r", CompactionKey: "k", CompactionRank: 5, CreatedAt: time.Now()}
	want := MessageMeta{Id: row.Id, RoutingKey: row.RoutingKey, CompactionKey: row.CompactionKey, CompactionRank: row.CompactionRank, CreatedAt: row.CreatedAt}
	if got := row.toMessageMeta(); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestClaimedExceptionToMessageMeta(t *testing.T) {
	exception := ClaimedException{MessageId: 9, RoutingKey: "r2", CompactionKey: "k2", CompactionRank: -1, CreatedAt: time.Now()}
	want := MessageMeta{Id: exception.MessageId, RoutingKey: exception.RoutingKey, CompactionKey: exception.CompactionKey, CompactionRank: exception.CompactionRank, CreatedAt: exception.CreatedAt}
	if got := exception.toMessageMeta(); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
