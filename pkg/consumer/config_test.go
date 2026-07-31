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
		Message:    &common.MessageOptions{Timeout: 10 * time.Second, Retry: &retry.Policy{MaxRetries: 5}},
		MessageMax: &common.MessageOptions{Timeout: time.Minute, Retry: &retry.Policy{BaseDelay: 5 * time.Second}},
	}).WithDefaults()

	msg := &common.MessageOptions{Timeout: time.Hour, Retry: &retry.Policy{BaseDelay: 3 * time.Second}}
	got := cfg.resolveMessageOptions(msg)

	if got.Timeout != time.Minute {
		t.Fatalf("Timeout = %v, want the MessageMax ceiling %v", got.Timeout, time.Minute)
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
	if got.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want the default %v", got.Timeout, 30*time.Second)
	}
	if got.Retry.MaxRetries != 3 {
		t.Fatalf("Retry.MaxRetries = %d, want the default 3", got.Retry.MaxRetries)
	}
}

func TestResolveMessageOptionsCeilingDefaultsToMessage(t *testing.T) {
	// MessageMax unset fills from Message: requests above the group defaults
	// clamp unless the ceiling is raised explicitly
	cfg := (&ConsumerConfig{
		Message: &common.MessageOptions{Timeout: 10 * time.Second},
	}).WithDefaults()

	got := cfg.resolveMessageOptions(&common.MessageOptions{Timeout: time.Hour})
	if got.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %v, want the default ceiling %v", got.Timeout, 10*time.Second)
	}
	if cfg.MessageMax.Timeout != 10*time.Second {
		t.Fatalf("MessageMax.Timeout = %v, want filled from Message %v", cfg.MessageMax.Timeout, 10*time.Second)
	}
}

func TestValidateRejectsBoundsOutOfOrder(t *testing.T) {
	// Message above MessageMax
	cfg := &ConsumerConfig{
		Message:    &common.MessageOptions{Timeout: time.Minute},
		MessageMax: &common.MessageOptions{Timeout: time.Second},
	}
	if err := cfg.WithDefaults().Validate(); err == nil {
		t.Fatal("Message.Timeout above MessageMax: want error, got nil")
	}

	// MessageMin above Message
	cfg = &ConsumerConfig{
		Message:    &common.MessageOptions{Timeout: time.Second},
		MessageMin: &common.MessageOptions{Timeout: time.Minute},
	}
	if err := cfg.WithDefaults().Validate(); err == nil {
		t.Fatal("MessageMin.Timeout above Message: want error, got nil")
	}
}

func TestValidateRejectsConcurrencyInBounds(t *testing.T) {
	for _, field := range []string{"MessageMin", "MessageMax"} {
		cfg := &ConsumerConfig{}
		bound := &common.MessageOptions{Concurrency: common.ConcurrencyDefer}
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
