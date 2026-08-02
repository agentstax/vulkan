package topic

import (
	"testing"
	"time"
)

func TestConfigWithDefaults(t *testing.T) {
	c := (&Config{}).WithDefaults()
	if c.PartitionSize != 1_000_000 {
		t.Fatalf("PartitionSize default = %d, want 1_000_000", c.PartitionSize)
	}
	if c.IdempotencyKeyTTL != time.Hour {
		t.Fatalf("IdempotencyKeyTTL default = %v, want 1h", c.IdempotencyKeyTTL)
	}

	set := (&Config{IdempotencyKeyTTL: 10 * time.Minute}).WithDefaults()
	if set.IdempotencyKeyTTL != 10*time.Minute {
		t.Fatalf("IdempotencyKeyTTL = %v, want the caller's 10m kept", set.IdempotencyKeyTTL)
	}
}

func TestConfigValidate(t *testing.T) {
	c := (&Config{RetentionTTL: -1 * time.Second}).WithDefaults()
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted a negative RetentionTTL")
	}
	if err := (&Config{}).WithDefaults().Validate(); err != nil {
		t.Fatalf("defaulted config: want nil, got %v", err)
	}
}

func TestAlterConfigValidate(t *testing.T) {
	ttl := 10 * time.Minute
	if err := (&AlterConfig{IdempotencyKeyTTL: &ttl}).Validate(); err != nil {
		t.Fatalf("single-field alter rejected: %v", err)
	}

	zero := time.Duration(0)
	if err := (&AlterConfig{IdempotencyKeyTTL: &zero}).Validate(); err == nil {
		t.Fatal("Validate accepted IdempotencyKeyTTL = 0")
	}

	if err := (&AlterConfig{}).Validate(); err == nil {
		t.Fatal("Validate accepted an empty alter")
	}
}

func TestConfigToTopic(t *testing.T) {
	c := (&Config{IdempotencyKeyTTL: 10 * time.Minute}).WithDefaults()
	topic := c.ToTopic(1, 1, "t", SchemaVersion(1), time.Now(), time.Now())
	if topic.IdempotencyKeyTTL != 10*time.Minute {
		t.Fatalf("ToTopic IdempotencyKeyTTL = %v, want 10m", topic.IdempotencyKeyTTL)
	}
	if topic.SchemaVersion != 1 {
		t.Fatalf("ToTopic SchemaVersion = %v, want 1", topic.SchemaVersion)
	}
}
