package metrics

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

func TestDefinitionsCarriesRegisteredMetadata(t *testing.T) {
	definitions := Definitions()
	backlog := definitionByName(t, definitions, MetricCursorBacklog.Name)

	if backlog.Code != "VK0083" || backlog.Kind != MetricKindGauge || backlog.Unit != MetricUnitCount("message") {
		t.Fatalf("definition = %+v", backlog)
	}
	if backlog.Scope != diagnostic.MetricScopeConsumerGroup {
		t.Fatalf("scope = %q", backlog.Scope)
	}
	if len(backlog.AttributeKeys) != 2 || backlog.AttributeKeys[0] != "topic" || backlog.AttributeKeys[1] != "group" {
		t.Fatalf("attribute keys = %v", backlog.AttributeKeys)
	}
}

func TestDefinitionsFiltersScopes(t *testing.T) {
	definitions := Definitions(diagnostic.MetricScopeTopic, diagnostic.MetricScopeConsumerSession)
	for _, definition := range definitions {
		if definition.Scope != diagnostic.MetricScopeTopic && definition.Scope != diagnostic.MetricScopeConsumerSession {
			t.Fatalf("definition %s has unrequested scope %q", definition.Name, definition.Scope)
		}
	}
	if len(definitions) != 11 {
		t.Fatalf("got %d definitions, want 11", len(definitions))
	}
}

func TestDefinitionsReturnsDefensiveAttributeKeys(t *testing.T) {
	first := Definitions(diagnostic.MetricScopeConsumerGroup)
	backlog := definitionByName(t, first, MetricCursorBacklog.Name)
	backlog.AttributeKeys[0] = "changed"

	second := Definitions(diagnostic.MetricScopeConsumerGroup)
	backlog = definitionByName(t, second, MetricCursorBacklog.Name)
	if backlog.AttributeKeys[0] != "topic" {
		t.Fatalf("definition mutated catalog state: %v", backlog.AttributeKeys)
	}
}

func TestDefinitionsOrderedByCode(t *testing.T) {
	definitions := Definitions()
	for i := 1; i < len(definitions); i++ {
		if definitions[i-1].Code >= definitions[i].Code {
			t.Fatalf("codes out of order: %s before %s", definitions[i-1].Code, definitions[i].Code)
		}
	}
}

func definitionByName(t *testing.T, definitions []MetricDefinition, name string) MetricDefinition {
	t.Helper()
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("definition %q not found", name)
	return MetricDefinition{}
}
