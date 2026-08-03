package consumer

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/datastore"
)

type testMessage struct {
	Name string `json:"name"`
}

func TestNewConsumerValidation(t *testing.T) {
	ds := &datastore.PostgresDatastore{}

	if _, err := NewConsumer[testMessage]("group", "", 1, ds, nil); err == nil {
		t.Fatal("expected error for empty topic name")
	}
	if _, err := NewConsumer[testMessage]("group", "topic", 0, ds, nil); err == nil {
		t.Fatal("expected error for SchemaVersion < 1")
	}
	if _, err := NewConsumer[testMessage]("group", "topic", 1, nil, nil); err == nil {
		t.Fatal("expected error for nil datastore")
	}
	if _, err := NewConsumer[testMessage]("group", "topic", 1, ds, &ConsumerConfig{Type: "BOGUS"}); err == nil {
		t.Fatal("expected error for invalid consumer type")
	}

	if _, err := NewConsumer[testMessage]("group", "topic", 1, ds, nil); err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
}

func TestNewMessageConsumerValidation(t *testing.T) {
	ds := &datastore.PostgresDatastore{}

	if _, err := NewMessageConsumer[testMessage]("group", "topic", 0, ds, nil); err == nil {
		t.Fatal("expected error for SchemaVersion < 1")
	}

	// the queue must fit a full batch
	if _, err := NewMessageConsumer[testMessage]("group", "topic", 1, ds, &ConsumerConfig{BatchLimit: 100, QueueSize: 8}); err == nil {
		t.Fatal("expected error for QueueSize < BatchLimit")
	}

	if _, err := NewMessageConsumer[testMessage]("group", "topic", 1, ds, nil); err != nil {
		t.Fatalf("NewMessageConsumer: %v", err)
	}
}

func TestNewExceptionConsumerValidation(t *testing.T) {
	ds := &datastore.PostgresDatastore{}

	if _, err := NewExceptionConsumer[testMessage]("group", "", 1, ds, nil); err == nil {
		t.Fatal("expected error for empty topic name")
	}
	if _, err := NewExceptionConsumer[testMessage]("group", "topic", 0, ds, nil); err == nil {
		t.Fatal("expected error for SchemaVersion < 1")
	}
	if _, err := NewExceptionConsumer[testMessage]("group", "topic", 1, nil, nil); err == nil {
		t.Fatal("expected error for nil datastore")
	}
	if _, err := NewExceptionConsumer[testMessage]("group", "topic", 1, ds, nil); err != nil {
		t.Fatalf("NewExceptionConsumer: %v", err)
	}
}
