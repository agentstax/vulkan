package consumer

import (
	"strings"
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/retry"
)

func TestResolveMessageOptionsPrecedence(t *testing.T) {
	// bounds > message > consumer defaults
	cfg := (&ConsumerConfig{
		Message:    &common.MessageOptions{WorkTimeout: 10 * time.Second, Retry: &retry.Policy{MaxRetries: 5}},
		MessageMax: &common.MessageOptions{WorkTimeout: time.Minute},
	}).WithDefaults()

	msg := &common.MessageOptions{WorkTimeout: time.Hour, Retry: &retry.Policy{BaseDelay: 3 * time.Second}}
	got := cfg.resolveMessageOptions(msg)

	if got.WorkTimeout != time.Minute {
		t.Fatalf("WorkTimeout = %v, want the MessageMax ceiling %v", got.WorkTimeout, time.Minute)
	}
	if got.Retry.BaseDelay != 3*time.Second {
		t.Fatalf("Retry.BaseDelay = %v, want the message's %v", got.Retry.BaseDelay, 3*time.Second)
	}
	if got.Retry.MaxRetries != 5 {
		t.Fatalf("Retry.MaxRetries = %d, want the consumer default 5", got.Retry.MaxRetries)
	}
}

func TestResolveMessageOptionsNilMessage(t *testing.T) {
	// a message with no options resolves to exactly the consumer defaults
	cfg := (&ConsumerConfig{}).WithDefaults()

	got := cfg.resolveMessageOptions(nil)
	if got.WorkTimeout != 30*time.Second {
		t.Fatalf("WorkTimeout = %v, want the default %v", got.WorkTimeout, 30*time.Second)
	}
	if got.Retry.MaxRetries != 3 {
		t.Fatalf("Retry.MaxRetries = %d, want the default 3", got.Retry.MaxRetries)
	}
}

func TestValidateRejectsConcurrencyInBounds(t *testing.T) {
	for _, field := range []string{"MessageMin", "MessageMax"} {
		cfg := &ConsumerConfig{}
		bound := &common.MessageOptions{Concurrency: common.ConcurrencyForbid}
		if field == "MessageMin" {
			cfg.MessageMin = bound
		} else {
			cfg.MessageMax = bound
		}
		err := cfg.WithDefaults().Validate()
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("%s with Concurrency: err = %v, want error naming %s", field, err, field)
		}
	}
}
