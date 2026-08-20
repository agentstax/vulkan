package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryableChecksRecoveryFirst(t *testing.T) {
	if !IsRetryable(errTestConnection.With("host", "db.local")) {
		t.Fatal("Transient error not retryable")
	}
	if IsRetryable(errTestTopicMissing.With("topic", "orders")) {
		t.Fatal("Permanent error retryable")
	}
	if !IsRetryable(fmt.Errorf("list topics: %w", errTestConnection)) {
		t.Fatal("fmt.Errorf-wrapped Transient error not retryable")
	}
}

func TestClassifyPassesRecoveryThrough(t *testing.T) {
	raised := errTestConnection.With("host", "db.local")
	if classify(raised) != error(raised) {
		t.Fatal("classify wrapped an error that already carries recovery")
	}

	wrapped := fmt.Errorf("list topics: %w", errTestTopicMissing)
	if classify(wrapped) != wrapped {
		t.Fatal("classify wrapped a chain that already carries recovery")
	}
}

func TestClassifyKeepsMarkerTypes(t *testing.T) {
	retryable := NewRetryableError(errors.New("plain cause"))
	if classify(retryable) != error(retryable) {
		t.Fatal("classify rewrapped a RetryableError")
	}

	permanent := NewPermanentError(errors.New("plain cause"))
	if classify(permanent) != error(permanent) {
		t.Fatal("classify rewrapped a PermanentError")
	}
}

func TestClassifyWrapsUnclassifiedErrors(t *testing.T) {
	deadlock := classify(&pgconn.PgError{Code: "40P01"})
	if _, ok := errors.AsType[*RetryableError](deadlock); !ok {
		t.Fatal("deadlock not classified retryable")
	}

	plain := classify(errors.New("no classification anywhere"))
	if _, ok := errors.AsType[*PermanentError](plain); !ok {
		t.Fatal("unclassifiable error not classified permanent")
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
	retryDatastore, err := NewRetryDatastore(policy, NewDefaultLogger(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return retryDatastore
}
