package diagnostic

import (
	"fmt"
	"slices"
)

// MetricScope names the resource identity carried by a built-in metric.
type MetricScope string

const (
	MetricScopeSystem          MetricScope = "system"
	MetricScopeTopic           MetricScope = "topic"
	MetricScopeConsumerGroup   MetricScope = "consumer_group"
	MetricScopeConsumerSession MetricScope = "consumer_session"
)

// Validate rejects a scope outside Vulkan's built-in resource scopes.
func (s MetricScope) Validate() error {
	switch s {
	case MetricScopeSystem, MetricScopeTopic, MetricScopeConsumerGroup, MetricScopeConsumerSession:
		return nil
	default:
		return fmt.Errorf("scope must be a Vulkan metric scope, got %q", s)
	}
}

// DiagnosticMetric is a declared Vulkan-owned metric: the identity and
// metadata every producer and rendering surface shares.
type DiagnosticMetric struct {
	Code          string
	Name          string
	Kind          string // "gauge" | "counter"
	Unit          string // UCUM code -- "" if none
	Description   string
	Scope         MetricScope
	AttributeKeys []string
}

// NewDiagnosticMetric declares a metric and registers its code. The name must be
// unique too -- a measurement on the wire carries only its name, and
// GetMetric resolves the declaration by that handle.
func NewDiagnosticMetric(code string, name string, kind string, unit string, description string, scope MetricScope, attributeKeys ...string) *DiagnosticMetric {
	if name == "" {
		panic("name must not be empty: " + code)
	}
	if kind == "" {
		panic("kind must not be empty: " + code)
	}
	if description == "" {
		panic("description must not be empty: " + code)
	}
	if err := scope.Validate(); err != nil {
		panic(err.Error() + ": " + code)
	}
	seenAttributeKeys := make(map[string]bool, len(attributeKeys))
	for _, attributeKey := range attributeKeys {
		if attributeKey == "" {
			panic("attribute key must not be empty: " + code)
		}
		if seenAttributeKeys[attributeKey] {
			panic("attribute key already declared: " + attributeKey + ": " + code)
		}
		seenAttributeKeys[attributeKey] = true
	}
	if existing, ok := GetMetric(name); ok {
		panic("metric name already registered as " + existing.Code + ": " + name)
	}

	declared := &DiagnosticMetric{
		Code:          code,
		Name:          name,
		Kind:          kind,
		Unit:          unit,
		Description:   description,
		Scope:         scope,
		AttributeKeys: slices.Clone(attributeKeys),
	}
	register(declared)
	return declared
}

// Docs returns the metric's documentation page, derived from the code.
func (m *DiagnosticMetric) Docs() string {
	return docsBaseURL + m.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (m *DiagnosticMetric) GetCode() string {
	return m.Code
}

func (m *DiagnosticMetric) GetKind() DiagnosticKind {
	return DiagnosticKindMetric
}

// Metrics lists every registered metric ordered by code.
func Metrics() []*DiagnosticMetric {
	return listRegistered[*DiagnosticMetric]()
}

// GetMetric returns the declaration behind a metric name; comma-ok absence
// for names the registry does not know (user metrics on the same topic).
// The registry fills once at init and stays small, so a scan serves.
func GetMetric(name string) (*DiagnosticMetric, bool) {
	registryLock.Lock()
	defer registryLock.Unlock()

	for _, registered := range registeredDeclarations {
		if declared, ok := registered.(*DiagnosticMetric); ok && declared.Name == name {
			return declared, true
		}
	}
	return nil, false
}
