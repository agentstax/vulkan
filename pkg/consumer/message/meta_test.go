package message

import (
	"context"
	"testing"
	"time"
)

func TestMetaFromContextBareContext(t *testing.T) {
	if _, ok := MetaFromContext(context.Background()); ok {
		t.Fatal("expected ok=false on a context with no meta set")
	}
}

func TestMetaFromContextRoundTrip(t *testing.T) {
	want := MessageMeta{Id: 42, RoutingKey: "orders.created", CompactionKey: "user:1", CompactionRank: 7, CreatedAt: time.Now()}
	ctx := WithMeta(context.Background(), want)

	got, ok := MetaFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true after WithMeta")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
