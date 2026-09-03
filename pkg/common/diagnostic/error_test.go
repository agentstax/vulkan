package diagnostic

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// Package-level declarations mirror real usage: registered once at init,
// walked by the tense and banned-word tests below alongside every other
// registered error.
var (
	errTestTopicMissing = NewDiagnosticError("VK9901", RecoveryPermanent,
		"test topic not found",
		"register it with RegisterTopic first")
	errTestConnection = NewDiagnosticError("VK9902", RecoveryTransient,
		"could not reach the test broker", "")
)

func TestErrorRendersAllParts(t *testing.T) {
	raised := errTestTopicMissing.With("topic", "orders", "version", 3)

	want := `test topic not found: topic "orders", version 3 -- register it with RegisterTopic first [VK9901]`
	if raised.Error() != want {
		t.Fatalf("got %q, want %q", raised.Error(), want)
	}
}

func TestErrorRendersWithoutValues(t *testing.T) {
	want := "test topic not found -- register it with RegisterTopic first [VK9901]"
	if errTestTopicMissing.Error() != want {
		t.Fatalf("got %q, want %q", errTestTopicMissing.Error(), want)
	}
}

func TestErrorRendersWithoutFix(t *testing.T) {
	raised := errTestConnection.With("host", "db.local", "timeout", 5*time.Second)

	want := `could not reach the test broker: host "db.local", timeout 5s [VK9902]`
	if raised.Error() != want {
		t.Fatalf("got %q, want %q", raised.Error(), want)
	}
}

func TestErrorRendersWrappedCause(t *testing.T) {
	cause := errors.New("connection refused")
	raised := errTestConnection.With("host", "db.local").Wrap(cause)

	want := `could not reach the test broker: host "db.local" [VK9902]: connection refused`
	if raised.Error() != want {
		t.Fatalf("got %q, want %q", raised.Error(), want)
	}
}

func TestWithReturnsCopy(t *testing.T) {
	first := errTestTopicMissing.With("topic", "orders")
	second := errTestTopicMissing.With("topic", "payments")

	if len(errTestTopicMissing.values) != 0 {
		t.Fatal("With mutated the declared error's values")
	}
	if first.Error() == second.Error() {
		t.Fatal("two raises share values")
	}
}

func TestIsMatchesOnCode(t *testing.T) {
	raised := errTestTopicMissing.With("topic", "orders")
	if !errors.Is(raised, errTestTopicMissing) {
		t.Fatal("raised copy does not match its declaration")
	}

	wrapped := fmt.Errorf("list topics: %w", raised)
	if !errors.Is(wrapped, errTestTopicMissing) {
		t.Fatal("fmt.Errorf-wrapped copy does not match its declaration")
	}

	if errors.Is(raised, errTestConnection) {
		t.Fatal("distinct codes match")
	}
}

func TestUnwrapReturnsCause(t *testing.T) {
	cause := errors.New("connection refused")
	raised := errTestConnection.Wrap(cause)

	if !errors.Is(raised, cause) {
		t.Fatal("wrapped cause unreachable through errors.Is")
	}
	if errTestConnection.wrapped != nil {
		t.Fatal("Wrap mutated the declared error")
	}
}

func TestDocsDerivesFromCode(t *testing.T) {
	want := "https://vulkan-5ss.pages.dev/errors/VK9901"
	if errTestTopicMissing.Docs() != want {
		t.Fatalf("got %q, want %q", errTestTopicMissing.Docs(), want)
	}
}

func TestLogValueRendersPartsAsFields(t *testing.T) {
	raised := errTestTopicMissing.With("topic", "orders").Wrap(errors.New("row deleted"))

	fields := map[string]string{}
	for _, attribute := range raised.LogValue().Group() {
		fields[attribute.Key] = attribute.Value.String()
	}

	want := map[string]string{
		"code":     "VK9901",
		"problem":  "test topic not found",
		"recovery": "permanent",
		"docs":     "https://vulkan-5ss.pages.dev/errors/VK9901",
		"fix":      "register it with RegisterTopic first",
		"topic":    "orders",
		"cause":    "row deleted",
	}
	for key, value := range want {
		if fields[key] != value {
			t.Fatalf("field %q: got %q, want %q", key, fields[key], value)
		}
	}
}

func TestNewErrorRejectsDuplicateCode(t *testing.T) {
	expectPanic(t, func() {
		NewDiagnosticError("VK9901", RecoveryPermanent, "duplicate registration attempt", "")
	})
}

func TestNewErrorRejectsMalformedCode(t *testing.T) {
	for _, code := range []string{"", "VK1", "VK12345", "XX0001", "VK00a1", "vk0001"} {
		expectPanic(t, func() {
			NewDiagnosticError(code, RecoveryPermanent, "malformed code attempt", "")
		})
	}
}

func TestNewErrorRejectsUnrecognizedRecovery(t *testing.T) {
	expectPanic(t, func() {
		NewDiagnosticError("VK9903", DiagnosticRecovery("maybe"), "unrecognized recovery attempt", "")
	})
}

func TestNewErrorRejectsEmptyProblem(t *testing.T) {
	expectPanic(t, func() {
		NewDiagnosticError("VK9904", RecoveryPermanent, "", "")
	})
}

func expectPanic(t *testing.T, run func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	run()
}
