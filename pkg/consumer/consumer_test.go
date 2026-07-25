package consumer

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type testMessage struct {
	Name string `json:"name"`
}

func testQueueAndPool(t *testing.T) (*concurrency.PressureQueue[Buffered], *concurrency.WorkerPoolLimiter) {
	t.Helper()
	queue, err := concurrency.NewPressureQueue[Buffered](8)
	if err != nil {
		t.Fatalf("NewPressureQueue: %v", err)
	}
	pool, err := concurrency.NewWorkerPoolLimiter(2)
	if err != nil {
		t.Fatalf("NewWorkerPoolLimiter: %v", err)
	}
	return queue, pool
}

func TestNewConsumerValidation(t *testing.T) {
	queue, pool := testQueueAndPool(t)
	ds := &datastore.PostgresDatastore{}

	if _, err := NewConsumer[testMessage]("group", "", queue, pool, ds, nil); err == nil {
		t.Fatal("expected error for empty topic name")
	}
	if _, err := NewConsumer[testMessage]("group", "topic", nil, pool, ds, nil); err == nil {
		t.Fatal("expected error for nil queue")
	}
	if _, err := NewConsumer[testMessage]("group", "topic", queue, nil, ds, nil); err == nil {
		t.Fatal("expected error for nil poolLimiter")
	}
	if _, err := NewConsumer[testMessage]("group", "topic", queue, pool, nil, nil); err == nil {
		t.Fatal("expected error for nil datastore")
	}

	c, err := NewConsumer[testMessage]("group", "topic", queue, pool, ds, nil)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	if c.MessageConsumer == nil || c.ExceptionConsumer == nil || c.Maintainer == nil {
		t.Fatalf("bundle not fully composed: %+v", c)
	}
}

func TestNewMessageConsumerValidation(t *testing.T) {
	queue, pool := testQueueAndPool(t)
	ds := &datastore.PostgresDatastore{}

	// queue cap must fit a full batch
	if _, err := NewMessageConsumer[testMessage]("group", "topic", queue, pool, ds, &ConsumerConfig{BatchLimit: 100}); err == nil {
		t.Fatal("expected error for queue cap < BatchLimit")
	}

	if _, err := NewMessageConsumer[testMessage]("group", "topic", queue, pool, ds, nil); err != nil {
		t.Fatalf("NewMessageConsumer: %v", err)
	}
}

func TestNewExceptionConsumerValidation(t *testing.T) {
	ds := &datastore.PostgresDatastore{}

	if _, err := NewExceptionConsumer[testMessage]("group", "", ds, nil); err == nil {
		t.Fatal("expected error for empty topic name")
	}
	if _, err := NewExceptionConsumer[testMessage]("group", "topic", nil, nil); err == nil {
		t.Fatal("expected error for nil datastore")
	}
	if _, err := NewExceptionConsumer[testMessage]("group", "topic", ds, nil); err != nil {
		t.Fatalf("NewExceptionConsumer: %v", err)
	}
}
