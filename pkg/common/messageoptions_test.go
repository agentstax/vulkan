package common

import (
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/retry"
)

func TestFillSetFieldsWin(t *testing.T) {
	msg := &MessageOptions{Timeout: time.Minute}
	defaults := &MessageOptions{Concurrency: ConcurrencyDefer, Timeout: time.Second, Retry: &retry.Policy{MaxRetries: 5}}

	got := msg.Fill(defaults)
	if got.Timeout != time.Minute {
		t.Fatalf("Timeout = %v, want the message's %v", got.Timeout, time.Minute)
	}
	if got.Concurrency != ConcurrencyDefer {
		t.Fatalf("Concurrency = %q, want the default %q", got.Concurrency, ConcurrencyDefer)
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

func TestFillNeverAliasesItsInputs(t *testing.T) {
	defaults := &MessageOptions{Timeout: time.Second, Retry: &retry.Policy{MaxRetries: 5}}

	var msg *MessageOptions
	got := msg.Fill(defaults)
	got.Timeout = time.Minute
	got.Retry.MaxRetries = 9
	if defaults.Timeout != time.Second || defaults.Retry.MaxRetries != 5 {
		t.Fatalf("writes to Fill's result reached its defaults: %+v", defaults)
	}
}

func TestResolveConcurrency(t *testing.T) {
	msg := &MessageOptions{Concurrency: ConcurrencyDefer}
	if got := msg.ResolveConcurrency(""); got.Concurrency != ConcurrencyDefer {
		t.Fatalf("Concurrency = %q, want the message's %q kept", got.Concurrency, ConcurrencyDefer)
	}
	if got := msg.ResolveConcurrency(ConcurrencyAllow); got.Concurrency != ConcurrencyAllow {
		t.Fatalf("Concurrency = %q, want the override %q", got.Concurrency, ConcurrencyAllow)
	}
	if got := (&MessageOptions{}).ResolveConcurrency(""); got.Concurrency != ConcurrencyAllow {
		t.Fatalf("Concurrency = %q, want unset to resolve to %q", got.Concurrency, ConcurrencyAllow)
	}
}

func TestClampBoundsFieldWise(t *testing.T) {
	msg := &MessageOptions{
		Timeout: time.Hour,
		Retry:   &retry.Policy{MaxRetries: 100, BaseDelay: time.Millisecond},
	}
	min := &MessageOptions{Retry: &retry.Policy{BaseDelay: time.Second}}
	max := &MessageOptions{Timeout: time.Minute, Retry: &retry.Policy{MaxRetries: 10}}

	got := msg.Clamp(min, max)
	if got.Timeout != time.Minute {
		t.Fatalf("Timeout = %v, want ceiling %v", got.Timeout, time.Minute)
	}
	if got.Retry.MaxRetries != 10 {
		t.Fatalf("Retry.MaxRetries = %d, want ceiling 10", got.Retry.MaxRetries)
	}
	if got.Retry.BaseDelay != time.Second {
		t.Fatalf("Retry.BaseDelay = %v, want floor %v", got.Retry.BaseDelay, time.Second)
	}
}

func TestClampZeroBoundsAreUnconstrained(t *testing.T) {
	msg := &MessageOptions{Timeout: time.Hour, Retry: &retry.Policy{MaxRetries: 100}}

	got := msg.Clamp(nil, nil)
	if got.Timeout != time.Hour || got.Retry.MaxRetries != 100 {
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
		{Timeout: -time.Second},
		{Retry: &retry.Policy{MaxRetries: -1}},
		{Retry: &retry.Policy{BaseDelay: time.Minute, MaxDelay: time.Second}},
	}
	for _, o := range invalid {
		if err := o.Validate(); err == nil {
			t.Fatalf("Validate(%+v) = nil, want error", o)
		}
	}
}

func TestNilFill(t *testing.T) {
	defaults := &MessageOptions{Timeout: time.Second}
	var msg *MessageOptions
	if got := msg.Fill(defaults); got == nil || got.Timeout != time.Second {
		t.Fatalf("nil.Fill(defaults) = %+v, want defaults' values", got)
	}
	if got := defaults.Fill(nil); got == nil || got.Timeout != time.Second {
		t.Fatalf("defaults.Fill(nil) = %+v, want the receiver's values", got)
	}
	if got := msg.Fill(nil); got != nil {
		t.Fatalf("nil.Fill(nil) = %+v, want nil", got)
	}
}
