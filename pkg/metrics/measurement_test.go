package metrics

import (
	"testing"
	"time"
)

func TestMeasurementKeyDeterministic(t *testing.T) {
	attributes := map[string]string{
		"topic":   "orders",
		"group":   "billing",
		"version": "1",
	}

	want := "vulkan.consumer.group.lag|group=billing,topic=orders,version=1"
	for range 100 {
		got := MeasurementKey("vulkan.consumer.group.lag", attributes)
		if got != want {
			t.Fatalf("MeasurementKey = %q, want %q", got, want)
		}
	}
}

func TestMeasurementKeyNoAttributes(t *testing.T) {
	got := MeasurementKey("vulkan.worker.state.unclaimed_workers", nil)
	if got != "vulkan.worker.state.unclaimed_workers" {
		t.Fatalf("MeasurementKey = %q, want bare name", got)
	}

	got = MeasurementKey("vulkan.worker.state.unclaimed_workers", map[string]string{})
	if got != "vulkan.worker.state.unclaimed_workers" {
		t.Fatalf("MeasurementKey with empty map = %q, want bare name", got)
	}
}

func TestMeasurementKeyDistinctAttributeSets(t *testing.T) {
	first := MeasurementKey("lag", map[string]string{"topic": "orders"})
	second := MeasurementKey("lag", map[string]string{"topic": "payments"})
	if first == second {
		t.Fatalf("distinct attribute values collided: %q", first)
	}

	bare := MeasurementKey("lag", nil)
	if first == bare {
		t.Fatalf("attributed key collided with bare name: %q", first)
	}
}

func TestNewMeasurementValidation(t *testing.T) {
	at := time.Now()

	if _, err := NewMeasurement("", KindGauge, 1, "", nil, at); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, err := NewMeasurement("lag", Kind("histogram"), 1, "", nil, at); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := NewMeasurement("lag", KindGauge, 1, "", nil, time.Time{}); err == nil {
		t.Fatal("zero at accepted")
	}

	measurement, err := NewMeasurement("lag", KindGauge, 42, "{message}", map[string]string{"topic": "orders"}, at)
	if err != nil {
		t.Fatalf("valid measurement rejected: %v", err)
	}
	if measurement.Value != 42 || measurement.Unit != "{message}" {
		t.Fatalf("fields not carried: %+v", measurement)
	}
}

func TestUnitValidate(t *testing.T) {
	for _, unit := range []Unit{"", "ms", "By", UnitMilliseconds, UnitCount("worker"), "By/{request}"} {
		if err := unit.Validate(); err != nil {
			t.Fatalf("unit %q rejected: %v", unit, err)
		}
	}
	for _, unit := range []Unit{"per worker", "{worker", "worker}", "{}", "{{worker}}", "{a} b"} {
		if err := unit.Validate(); err == nil {
			t.Fatalf("unit %q accepted", unit)
		}
	}
}

func TestNewMeasurementRejectsMalformedUnit(t *testing.T) {
	if _, err := NewMeasurement("lag", KindGauge, 1, "{", nil, time.Now()); err == nil {
		t.Fatal("malformed unit accepted")
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
