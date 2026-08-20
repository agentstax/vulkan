package diagnostic

import (
	"strings"
	"testing"
)

var eventTestReclaim = NewEvent("VK9910",
	"test lease reclaimed", "delivery returns to the queue")

func TestNewEventAppendsConsequence(t *testing.T) {
	if eventTestReclaim.Message != "test lease reclaimed -- delivery returns to the queue" {
		t.Fatalf("message renders %q", eventTestReclaim.Message)
	}
}

func TestNewEventRejectsRegisteredErrorCode(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("reusing an error code did not panic")
		}
		if !strings.Contains(recovered.(string), "registered as error") {
			t.Fatalf("panic message: %v", recovered)
		}
	}()
	NewEvent("VK9901", "duplicate registration attempt", "")
}

func TestNewErrorRejectsRegisteredEventCode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("reusing a log event code did not panic")
		}
	}()
	NewError("VK9910", Permanent, "duplicate registration attempt", "")
}

func TestEventsListsOrderedByCode(t *testing.T) {
	listed := Events()
	for i := 1; i < len(listed); i++ {
		if listed[i-1].Code >= listed[i].Code {
			t.Fatalf("codes out of order: %s before %s", listed[i-1].Code, listed[i].Code)
		}
	}
}
