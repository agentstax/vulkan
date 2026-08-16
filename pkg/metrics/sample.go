package metrics

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// sample names published by the metrics collector worker
const (
	SampleUnclaimedWorkers   = "vulkan.worker.state.unclaimed_workers"    // workers with no live instance and a nonzero target
	SampleOldestUnclaimedAge = "vulkan.worker.state.oldest_unclaimed_age" // largest now() - expires_at among unclaimed workers, in ms
	SampleFailingWorkers     = "vulkan.worker.state.failing_workers"      // workers with a live instance on a nonzero failure streak
)

type Kind string

const (
	KindGauge   Kind = "gauge"   // a point-in-time level, each sample replaces the last
	KindCounter Kind = "counter" // a running total, each sample carries the new total
)

func (k Kind) Validate() error {
	switch k {
	case KindGauge, KindCounter:
		return nil
	default:
		return fmt.Errorf("kind must be %q or %q, got %q", KindGauge, KindCounter, k)
	}
}

// Unit is a metric's UCUM code. A real unit ("ms", "s", "By") carries a
// dimension a reader may format (47000 ms -> 47s); a braced annotation
// ("{worker}", via UnitCount) is a dimensionless count whose text is a human
// label only. "" is no unit.
type Unit string

const UnitMilliseconds Unit = "ms"

// UnitCount is the UCUM annotation for a dimensionless count of noun.
// Ex: UnitCount("worker") -> "{worker}"
func UnitCount(noun string) Unit {
	return Unit("{" + noun + "}")
}

// Validate checks UCUM shape -- the unit set is open, so
// only whitespace and malformed annotations can be rejected.
func (u Unit) Validate() error {
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

// Sample is one metric point on the __system.metrics topic. Names starting
// with "vulkan." are reserved for Vulkan's own samples.
type Sample struct {
	Name       string            `json:"name"`
	Kind       Kind              `json:"kind"`
	Value      float64           `json:"value"`
	Unit       Unit              `json:"unit"`
	Attributes map[string]string `json:"attributes"`
	At         time.Time         `json:"at"`
}

func NewSample(name string, kind Kind, value float64, unit Unit, attributes map[string]string, at time.Time) (*Sample, error) {
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

	return &Sample{
		Name:       name,
		Kind:       kind,
		Value:      value,
		Unit:       unit,
		Attributes: attributes,
		At:         at,
	}, nil
}

// SampleKey is the compaction key a Sample is produced under. Attribute keys
// are sorted, so equal attribute sets always yield one key -- map iteration
// order must never reach it.
//
// Ex: ("lag", {"group": "billing", "topic": "orders"}) -> "lag|group=billing,topic=orders"
// Ex: ("lag", nil) -> "lag"
func SampleKey(name string, attributes map[string]string) string {
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
