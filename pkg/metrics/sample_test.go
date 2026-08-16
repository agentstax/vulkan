package metrics

import (
	"testing"
	"time"
)

func TestSampleKeyDeterministic(t *testing.T) {
	attributes := map[string]string{
		"topic":   "orders",
		"group":   "billing",
		"version": "1",
	}

	want := "vulkan.consumer.group.lag|group=billing,topic=orders,version=1"
	for range 100 {
		got := SampleKey("vulkan.consumer.group.lag", attributes)
		if got != want {
			t.Fatalf("SampleKey = %q, want %q", got, want)
		}
	}
}

func TestSampleKeyNoAttributes(t *testing.T) {
	got := SampleKey("vulkan.worker.state.unclaimed_workers", nil)
	if got != "vulkan.worker.state.unclaimed_workers" {
		t.Fatalf("SampleKey = %q, want bare name", got)
	}

	got = SampleKey("vulkan.worker.state.unclaimed_workers", map[string]string{})
	if got != "vulkan.worker.state.unclaimed_workers" {
		t.Fatalf("SampleKey with empty map = %q, want bare name", got)
	}
}

func TestSampleKeyDistinctAttributeSets(t *testing.T) {
	first := SampleKey("lag", map[string]string{"topic": "orders"})
	second := SampleKey("lag", map[string]string{"topic": "payments"})
	if first == second {
		t.Fatalf("distinct attribute values collided: %q", first)
	}

	bare := SampleKey("lag", nil)
	if first == bare {
		t.Fatalf("attributed key collided with bare name: %q", first)
	}
}

func TestNewSampleValidation(t *testing.T) {
	at := time.Now()

	if _, err := NewSample("", KindGauge, 1, "", nil, at); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, err := NewSample("lag", Kind("histogram"), 1, "", nil, at); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := NewSample("lag", KindGauge, 1, "", nil, time.Time{}); err == nil {
		t.Fatal("zero at accepted")
	}

	sample, err := NewSample("lag", KindGauge, 42, "{message}", map[string]string{"topic": "orders"}, at)
	if err != nil {
		t.Fatalf("valid sample rejected: %v", err)
	}
	if sample.Value != 42 || sample.Unit != "{message}" {
		t.Fatalf("fields not carried: %+v", sample)
	}
}

func TestKindValidate(t *testing.T) {
	if err := KindGauge.Validate(); err != nil {
		t.Fatalf("gauge rejected: %v", err)
	}
	if err := KindCounter.Validate(); err != nil {
		t.Fatalf("counter rejected: %v", err)
	}
	if err := Kind("").Validate(); err == nil {
		t.Fatal("empty kind accepted")
	}
}
