package topic

import (
	"testing"
	"time"
)

func TestConfigWithDefaultsWaterlinePollRate(t *testing.T) {
	c := (&Config{}).WithDefaults()
	if c.WaterlinePollRate != 1*time.Second {
		t.Fatalf("WaterlinePollRate default = %v, want 1s", c.WaterlinePollRate)
	}

	set := (&Config{WaterlinePollRate: 250 * time.Millisecond}).WithDefaults()
	if set.WaterlinePollRate != 250*time.Millisecond {
		t.Fatalf("WaterlinePollRate = %v, want the caller's 250ms kept", set.WaterlinePollRate)
	}
}

func TestConfigValidateWaterlinePollRate(t *testing.T) {
	c := (&Config{WaterlinePollRate: -1 * time.Second}).WithDefaults()
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted a negative WaterlinePollRate")
	}
}

func TestAlterConfigWaterlinePollRate(t *testing.T) {
	rate := 2 * time.Second
	if err := (&AlterConfig{WaterlinePollRate: &rate}).Validate(); err != nil {
		t.Fatalf("waterline-only alter rejected: %v", err)
	}

	zero := time.Duration(0)
	if err := (&AlterConfig{WaterlinePollRate: &zero}).Validate(); err == nil {
		t.Fatal("Validate accepted WaterlinePollRate = 0")
	}

	if err := (&AlterConfig{}).Validate(); err == nil {
		t.Fatal("Validate accepted an empty alter")
	}
}

func TestConfigToTopicCarriesWaterlinePollRate(t *testing.T) {
	c := (&Config{WaterlinePollRate: 3 * time.Second}).WithDefaults()
	topic := c.ToTopic(1, 1, "t", SchemaVersion(1), time.Now(), time.Now())
	if topic.WaterlinePollRate != 3*time.Second {
		t.Fatalf("ToTopic WaterlinePollRate = %v, want 3s", topic.WaterlinePollRate)
	}
	if topic.SchemaVersion != 1 {
		t.Fatalf("ToTopic SchemaVersion = %v, want 1", topic.SchemaVersion)
	}
}
