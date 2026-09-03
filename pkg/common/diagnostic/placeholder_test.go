package diagnostic

import (
	"log/slog"
	"slices"
	"testing"
)

var errTestFixSubstitutes = NewDiagnosticError("VK9903", RecoveryPermanent,
	"test schema version is older than this build requires",
	"migrate the {owner_kind} schema up from {version} to {build_version}")

func TestErrorFillsFixFromAttachedValues(t *testing.T) {
	raised := errTestFixSubstitutes.With("owner_kind", "topic", "version", 4, "build_version", 7)

	want := `test schema version is older than this build requires: owner_kind "topic", version 4, build_version 7 -- migrate the topic schema up from 4 to 7 [VK9903]`
	if raised.Error() != want {
		t.Fatalf("got %q, want %q", raised.Error(), want)
	}
}

func TestFixSubstitutionKeepsTheValueRaw(t *testing.T) {
	declared := NewDiagnosticError("VK9904", RecoveryPermanent,
		"test topic not found",
		`register "{topic}" with RegisterTopic first`)
	raised := declared.With("topic", "orders")

	want := `test topic not found: topic "orders" -- register "orders" with RegisterTopic first [VK9904]`
	if raised.Error() != want {
		t.Fatalf("got %q, want %q", raised.Error(), want)
	}
}

func TestUnattachedPlaceholderStaysLiteral(t *testing.T) {
	raised := errTestFixSubstitutes.With("owner_kind", "topic")

	want := `test schema version is older than this build requires: owner_kind "topic" -- migrate the topic schema up from {version} to {build_version} [VK9903]`
	if raised.Error() != want {
		t.Fatalf("got %q, want %q", raised.Error(), want)
	}
}

func TestLogValueFillsTheFix(t *testing.T) {
	raised := errTestFixSubstitutes.With("owner_kind", "system", "version", 1, "build_version", 2)

	var filled string
	for _, attribute := range raised.LogValue().Group() {
		if attribute.Key == "fix" {
			filled = attribute.Value.String()
		}
	}

	want := "migrate the system schema up from 1 to 2"
	if filled != want {
		t.Fatalf("got %q, want %q", filled, want)
	}
}

func TestFixPlaceholdersListsEachNameOnce(t *testing.T) {
	declared := NewDiagnosticError("VK9905", RecoveryPermanent,
		"test topic not found",
		"register {topic} again, or destroy {topic} first")

	want := []string{"topic"}
	if got := declared.FixPlaceholders(); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFixPlaceholdersIsEmptyForAStaticFix(t *testing.T) {
	if got := errTestTopicMissing.FixPlaceholders(); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

// a jsonb containment literal in a query, a Go composite literal in a fix
func TestFillLeavesANonAttributeBraceRunAlone(t *testing.T) {
	values := []slog.Attr{slog.String("topic", "orders")}

	if got := fillPlaceholders(`payload @> '{}'`, values); got != `payload @> '{}'` {
		t.Fatalf("got %q, want the text unchanged", got)
	}
}
