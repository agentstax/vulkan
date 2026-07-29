package common

import (
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/retry"
)

func TestFillSetFieldsWin(t *testing.T) {
	msg := &MessageOptions{WorkTimeout: time.Minute}
	defaults := &MessageOptions{Concurrency: ConcurrencyForbid, WorkTimeout: time.Second, Retry: &retry.Policy{MaxRetries: 5}}

	got := msg.Fill(defaults)
	if got.WorkTimeout != time.Minute {
		t.Fatalf("WorkTimeout = %v, want the message's %v", got.WorkTimeout, time.Minute)
	}
	if got.Concurrency != ConcurrencyForbid {
		t.Fatalf("Concurrency = %q, want the default %q", got.Concurrency, ConcurrencyForbid)
	}
	if got.Retry == nil || got.Retry.MaxRetries != 5 {
		t.Fatalf("Retry = %+v, want the default policy", got.Retry)
	}
}

func TestFillRetryMergesFieldWise(t *testing.T) {
	msg := &MessageOptions{Retry: &retry.Policy{BaseDelay: 10 * time.Second}}
	defaults := &MessageOptions{Retry: &retry.Policy{MaxRetries: 5, BaseDelay: time.Second}}

	got := msg.Fill(defaults)
	if got.Retry.BaseDelay != 10*time.Second || got.Retry.MaxRetries != 5 {
		t.Fatalf("Retry = %+v, want BaseDelay from the message, MaxRetries from defaults", got.Retry)
	}
	// merge must not have written through the message's own policy
	if msg.Retry.MaxRetries != 0 {
		t.Fatalf("Fill mutated its input policy: %+v", msg.Retry)
	}
}

func TestClampBoundsFieldWise(t *testing.T) {
	msg := &MessageOptions{
		WorkTimeout: time.Hour,
		Retry:       &retry.Policy{MaxRetries: 100, BaseDelay: time.Millisecond},
	}
	min := &MessageOptions{Retry: &retry.Policy{BaseDelay: time.Second}}
	max := &MessageOptions{WorkTimeout: time.Minute, Retry: &retry.Policy{MaxRetries: 10}}

	got := msg.Clamp(min, max)
	if got.WorkTimeout != time.Minute {
		t.Fatalf("WorkTimeout = %v, want ceiling %v", got.WorkTimeout, time.Minute)
	}
	if got.Retry.MaxRetries != 10 {
		t.Fatalf("Retry.MaxRetries = %d, want ceiling 10", got.Retry.MaxRetries)
	}
	if got.Retry.BaseDelay != time.Second {
		t.Fatalf("Retry.BaseDelay = %v, want floor %v", got.Retry.BaseDelay, time.Second)
	}
}

func TestClampZeroBoundsAreUnconstrained(t *testing.T) {
	msg := &MessageOptions{WorkTimeout: time.Hour, Retry: &retry.Policy{MaxRetries: 100}}

	got := msg.Clamp(nil, nil)
	if got.WorkTimeout != time.Hour || got.Retry.MaxRetries != 100 {
		t.Fatalf("zero bounds changed the message: %+v", got)
	}
}

func TestValidateSparse(t *testing.T) {
	valid := []*MessageOptions{
		nil, // nil requests nothing -- always valid
		{Concurrency: ConcurrencyDefer},
		{Retry: &retry.Policy{MaxRetries: 2}}, // partial policy: unset fields are the consumer's
		{Retry: &retry.Policy{MaxDelay: 10 * time.Second}}, // MaxDelay alone -- no BaseDelay to undercut
	}
	for _, o := range valid {
		if err := o.Validate(); err != nil {
			t.Fatalf("Validate(%+v) = %v, want nil", o, err)
		}
	}

	invalid := []*MessageOptions{
		{Concurrency: "sometimes"},
		{WorkTimeout: -time.Second},
		{Retry: &retry.Policy{MaxRetries: -1}},
		{Retry: &retry.Policy{BaseDelay: time.Minute, MaxDelay: time.Second}},
	}
	for _, o := range invalid {
		if err := o.Validate(); err == nil {
			t.Fatalf("Validate(%+v) = nil, want error", o)
		}
	}
}

func TestNilFillIdentity(t *testing.T) {
	defaults := &MessageOptions{WorkTimeout: time.Second}
	var msg *MessageOptions
	if got := msg.Fill(defaults); got != defaults {
		t.Fatalf("nil.Fill(defaults) = %+v, want defaults verbatim", got)
	}
	if got := defaults.Fill(nil); got != defaults {
		t.Fatalf("defaults.Fill(nil) = %+v, want the receiver verbatim", got)
	}
}
