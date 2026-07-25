package maintain

import "testing"

func TestMaintainerConfigWithDefaults(t *testing.T) {
	c := (&MaintainerConfig{}).WithDefaults()
	if c.JitterFraction != 0.1 {
		t.Fatalf("JitterFraction default = %v, want 0.1", c.JitterFraction)
	}
	if c.Logger == nil || c.Retry == nil {
		t.Fatal("Logger/Retry defaults not filled")
	}

	set := (&MaintainerConfig{JitterFraction: 0.5}).WithDefaults()
	if set.JitterFraction != 0.5 {
		t.Fatalf("JitterFraction = %v, want the caller's 0.5 kept", set.JitterFraction)
	}
}

func TestMaintainerConfigValidateJitterFraction(t *testing.T) {
	for _, bad := range []float64{-0.1, 1, 1.5} {
		c := (&MaintainerConfig{JitterFraction: bad}).WithDefaults()
		if err := c.Validate(); err == nil {
			t.Fatalf("Validate accepted JitterFraction %v", bad)
		}
	}

	ok := (&MaintainerConfig{JitterFraction: 0.9}).WithDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatalf("Validate rejected JitterFraction 0.9: %v", err)
	}
}

func TestNewMaintainerValidation(t *testing.T) {
	if _, err := NewMaintainer(); err == nil {
		t.Fatal("NewMaintainer accepted an empty duty list")
	}
}

func TestNewJanitorValidation(t *testing.T) {
	if _, err := NewJanitor("", nil, nil); err == nil {
		t.Fatal("NewJanitor accepted an empty topic name")
	}
	if _, err := NewJanitor("t", nil, nil); err == nil {
		t.Fatal("NewJanitor accepted a nil datastore")
	}
}

func TestNewWaterlineRollerValidation(t *testing.T) {
	if _, err := NewWaterlineRoller("", "t", nil, nil); err == nil {
		t.Fatal("NewWaterlineRoller accepted an empty consumer group")
	}
	if _, err := NewWaterlineRoller("g", "", nil, nil); err == nil {
		t.Fatal("NewWaterlineRoller accepted an empty topic name")
	}
	if _, err := NewWaterlineRoller("g", "t", nil, nil); err == nil {
		t.Fatal("NewWaterlineRoller accepted a nil datastore")
	}
}
