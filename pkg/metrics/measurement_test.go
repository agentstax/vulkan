package metrics

import (
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
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

	if _, err := NewMeasurement("", MetricKindGauge, 1, "", nil, at); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, err := NewMeasurement("lag", MetricKind("histogram"), 1, "", nil, at); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := NewMeasurement("lag", MetricKindGauge, 1, "", nil, time.Time{}); err == nil {
		t.Fatal("zero at accepted")
	}

	measurement, err := NewMeasurement("lag", MetricKindGauge, 42, "{message}", map[string]string{"topic": "orders"}, at)
	if err != nil {
		t.Fatalf("valid measurement rejected: %v", err)
	}
	if measurement.Value != 42 || measurement.Unit != "{message}" {
		t.Fatalf("fields not carried: %+v", measurement)
	}
}

func TestUnitValidate(t *testing.T) {
	for _, unit := range []MetricUnit{"", "ms", "By", MetricUnitMilliseconds, MetricUnitCount("worker"), "By/{request}"} {
		if err := unit.Validate(); err != nil {
			t.Fatalf("unit %q rejected: %v", unit, err)
		}
	}
	for _, unit := range []MetricUnit{"per worker", "{worker", "worker}", "{}", "{{worker}}", "{a} b"} {
		if err := unit.Validate(); err == nil {
			t.Fatalf("unit %q accepted", unit)
		}
	}
}

func TestNewMeasurementRejectsMalformedUnit(t *testing.T) {
	if _, err := NewMeasurement("lag", MetricKindGauge, 1, "{", nil, time.Now()); err == nil {
		t.Fatal("malformed unit accepted")
	}
}

func TestNewBuiltInMeasurementUsesDeclaration(t *testing.T) {
	at := time.Now()
	attributes := map[string]string{"group": "billing", "topic": "orders"}

	measurement, err := NewBuiltInMeasurement(MetricCursorBacklog, 42, attributes, at)
	if err != nil {
		t.Fatalf("valid built-in measurement rejected: %v", err)
	}
	if measurement.Name != MetricCursorBacklog.Name || measurement.Kind != MetricKind(MetricCursorBacklog.Kind) || measurement.Unit != MetricUnit(MetricCursorBacklog.Unit) {
		t.Fatalf("declaration metadata not carried: %+v", measurement)
	}
	if measurement.Value != 42 || measurement.At != at {
		t.Fatalf("observed facts not carried: %+v", measurement)
	}
}

func TestNewBuiltInMeasurementRejectsWrongAttributeKeys(t *testing.T) {
	at := time.Now()
	tests := []struct {
		name       string
		attributes map[string]string
	}{
		{name: "missing", attributes: map[string]string{"topic": "orders"}},
		{name: "extra", attributes: map[string]string{"topic": "orders", "group": "billing", "session": "one"}},
		{name: "replacement", attributes: map[string]string{"topic": "orders", "session": "one"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewBuiltInMeasurement(MetricCursorBacklog, 42, test.attributes, at); err == nil {
				t.Fatal("wrong attribute keys accepted")
			}
		})
	}
}

func TestNewBuiltInMeasurementRejectsNilDeclaration(t *testing.T) {
	if _, err := NewBuiltInMeasurement(nil, 42, nil, time.Now()); err == nil {
		t.Fatal("nil declaration accepted")
	}
}

func TestNewMeasurementKeepsUserAttributesOpenEnded(t *testing.T) {
	attributes := map[string]string{"tenant": "north", "region": "east"}
	if _, err := NewMeasurement("application.request.count", MetricKindCounter, 42, MetricUnitCount("request"), attributes, time.Now()); err != nil {
		t.Fatalf("user measurement attributes rejected: %v", err)
	}
}

func TestNewBuiltInMeasurementRejectsDeclarationWithInvalidKind(t *testing.T) {
	declared := &diagnostic.DiagnosticMetric{
		Name:        "vulkan.test.invalid_kind",
		Kind:        "histogram",
		Description: "test invalid kind",
		Scope:       diagnostic.MetricScopeSystem,
	}
	if _, err := NewBuiltInMeasurement(declared, 42, nil, time.Now()); err == nil {
		t.Fatal("invalid declaration kind accepted")
	}
}

func TestKindValidate(t *testing.T) {
	if err := MetricKindGauge.Validate(); err != nil {
		t.Fatalf("gauge rejected: %v", err)
	}
	if err := MetricKindCounter.Validate(); err != nil {
		t.Fatalf("counter rejected: %v", err)
	}
	if err := MetricKind("").Validate(); err == nil {
		t.Fatal("empty kind accepted")
	}
}
