package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	errTestConnection = diagnostic.NewError("VK9905", diagnostic.Transient,
		"could not reach the test database", "")
	errTestTopicMissing = diagnostic.NewError("VK9906", diagnostic.Permanent,
		"test topic not found", "")
)

func TestIsTransientDatastoreError(t *testing.T) {
	if !IsTransientDatastoreError(errTestConnection.With("host", "db.local")) {
		t.Fatal("Transient recovery not transient")
	}
	if IsTransientDatastoreError(errTestTopicMissing.With("topic", "orders")) {
		t.Fatal("Permanent recovery transient")
	}
	if !IsTransientDatastoreError(fmt.Errorf("list topics: %w", errTestConnection)) {
		t.Fatal("fmt.Errorf-wrapped Transient recovery not transient")
	}

	// recovery wins over the wrapped cause's own classification
	if IsTransientDatastoreError(errTestTopicMissing.Wrap(&pgconn.PgError{Code: "40P01"})) {
		t.Fatal("Permanent recovery lost to its wrapped deadlock")
	}

	if !IsTransientDatastoreError(&pgconn.PgError{Code: "40P01"}) {
		t.Fatal("bare deadlock not transient")
	}
	if IsTransientDatastoreError(errors.New("no classification anywhere")) {
		t.Fatal("bare unclassifiable error transient")
	}
}

func TestRetryDatastoreStopsOnUnclassifiedError(t *testing.T) {
	retryDatastore := newTestRetryDatastore(t)

	attempts := 0
	err := retryDatastore.Wrap(context.Background(), func() error {
		attempts++
		return errors.New("no classification anywhere")
	})
	if err == nil || attempts != 1 {
		t.Fatalf("unclassifiable error retried: %d attempts", attempts)
	}
}

func TestRetryDatastoreStopsOnPermanentRecovery(t *testing.T) {
	retryDatastore := newTestRetryDatastore(t)

	attempts := 0
	err := retryDatastore.Wrap(context.Background(), func() error {
		attempts++
		return errTestTopicMissing.With("topic", "orders")
	})

	if !errors.Is(err, errTestTopicMissing) {
		t.Fatalf("declared error lost through Wrap: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("Permanent recovery retried: %d attempts", attempts)
	}
}

func TestRetryDatastoreRetriesOnTransientRecovery(t *testing.T) {
	retryDatastore := newTestRetryDatastore(t)

	attempts := 0
	err := retryDatastore.Wrap(context.Background(), func() error {
		attempts++
		return errTestConnection.With("host", "db.local")
	})

	if !errors.Is(err, errTestConnection) {
		t.Fatalf("declared error lost through Wrap: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("Transient recovery did not use the schedule: %d attempts", attempts)
	}
}

func newTestRetryDatastore(t *testing.T) *RetryDatastore {
	t.Helper()

	policy := &RetryPolicy{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Exponent: 1}
	retryDatastore, err := NewRetryDatastore(policy, logging.NewDefaultLogger(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return retryDatastore
}
