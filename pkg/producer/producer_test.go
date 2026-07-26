package producer

import "testing"

func TestProduceOptionsValidate(t *testing.T) {
	if err := (ProduceOptions{}).Validate(); err != nil {
		t.Fatalf("zero value must validate clean: %v", err)
	}
	if err := (ProduceOptions{CompactionKey: "k", CompactionRank: 5}).Validate(); err != nil {
		t.Fatalf("rank with a key must validate clean: %v", err)
	}
	if err := (ProduceOptions{CompactionRank: 5}).Validate(); err == nil {
		t.Fatal("expected an error for CompactionRank set without CompactionKey")
	}
	if err := (ProduceOptions{CompactionRank: -1}).Validate(); err == nil {
		t.Fatal("expected an error for a negative CompactionRank set without CompactionKey")
	}
}
