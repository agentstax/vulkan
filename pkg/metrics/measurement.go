package metrics

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// MetricNameReservedPrefix marks Vulkan's own metrics -- user producers
// must not use it.
const MetricNameReservedPrefix = "vulkan."

type MetricKind string

const (
	MetricKindGauge   MetricKind = "gauge"   // a point-in-time level, each measurement replaces the last
	MetricKindCounter MetricKind = "counter" // a running total, each measurement carries the new total
)

func (k MetricKind) Validate() error {
	switch k {
	case MetricKindGauge, MetricKindCounter:
		return nil
	default:
		return fmt.Errorf("kind must be %q or %q, got %q", MetricKindGauge, MetricKindCounter, k)
	}
}

// MetricUnit is a metric's UCUM code. A real unit ("ms", "s", "By") carries a
// dimension a reader may format (47000 ms -> 47s); a braced annotation
// ("{worker}", via MetricUnitCount) is a dimensionless count whose text is a human
// label only. "" is no unit.
type MetricUnit string

const MetricUnitMilliseconds MetricUnit = "ms"

// MetricUnitCount is the UCUM annotation for a dimensionless count of noun.
// Ex: MetricUnitCount("worker") -> "{worker}"
func MetricUnitCount(noun string) MetricUnit {
	return MetricUnit("{" + noun + "}")
}

// Validate checks UCUM shape -- the unit set is open, so
// only whitespace and malformed annotations can be rejected.
func (u MetricUnit) Validate() error {
	inAnnotation := false
	for _, character := range u {
		switch {
		case unicode.IsSpace(character):
			return fmt.Errorf("unit %q must not contain whitespace", u)
		case character == '{':
			if inAnnotation {
				return fmt.Errorf("unit %q nests an annotation", u)
			}
			inAnnotation = true
		case character == '}':
			if !inAnnotation {
				return fmt.Errorf("unit %q closes an unopened annotation", u)
			}
			inAnnotation = false
		}
	}
	if inAnnotation {
		return fmt.Errorf("unit %q leaves an annotation unclosed", u)
	}
	if strings.Contains(string(u), "{}") {
		return fmt.Errorf("unit %q has an empty annotation", u)
	}
	return nil
}

// Measurement is one value of one metric at one time, on the __system.metrics
// topic. Names starting with "vulkan." are reserved for Vulkan's own metrics.
type Measurement struct {
	Name       string            `json:"name"`
	Kind       MetricKind        `json:"kind"`
	Value      float64           `json:"value"`
	Unit       MetricUnit        `json:"unit"`
	Attributes map[string]string `json:"attributes"`
	At         time.Time         `json:"at"`
}

func (Measurement) SchemaVersion() int { return 1 }

func NewMeasurement(name string, kind MetricKind, value float64, unit MetricUnit, attributes map[string]string, at time.Time) (*Measurement, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if err := kind.Validate(); err != nil {
		return nil, err
	}
	if err := unit.Validate(); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, errors.New("at is required")
	}

	return &Measurement{
		Name:       name,
		Kind:       kind,
		Value:      value,
		Unit:       unit,
		Attributes: attributes,
		At:         at,
	}, nil
}

// MeasurementKey is the message key a Measurement is produced under. Attribute keys
// are sorted, so equal attribute sets always yield one key -- map iteration
// order must never reach it.
//
// Ex: ("lag", {"group": "billing", "topic": "orders"}) -> "lag|group=billing,topic=orders"
// Ex: ("lag", nil) -> "lag"
func MeasurementKey(name string, attributes map[string]string) string {
	if len(attributes) == 0 {
		return name
	}

	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString(name)
	for i, key := range keys {
		if i == 0 {
			builder.WriteString("|")
		} else {
			builder.WriteString(",")
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(attributes[key])
	}
	return builder.String()
}
