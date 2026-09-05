package alert

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

func TestDefinitionsCarriesRegisteredMetadata(t *testing.T) {
	definitions := Definitions()
	partitionCount := definitionByName(t, definitions, AlertPartitionCount.Name)

	if partitionCount.Code != "VK0094" || partitionCount.Severity != AlertSeverityWarn {
		t.Fatalf("definition = %+v", partitionCount)
	}
	if partitionCount.Scope != diagnostic.MetricScopeTopic {
		t.Fatalf("scope = %q", partitionCount.Scope)
	}
	if partitionCount.Description != AlertPartitionCount.Description {
		t.Fatalf("description = %q", partitionCount.Description)
	}
}

func TestDefinitionsFiltersScopes(t *testing.T) {
	if got := len(Definitions(diagnostic.MetricScopeTopic)); got != 3 {
		t.Fatalf("got %d topic definitions, want 3", got)
	}
	if got := len(Definitions(diagnostic.MetricScopeSystem, diagnostic.MetricScopeConsumerGroup)); got != 0 {
		t.Fatalf("got %d system/group definitions, want 0", got)
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

func definitionByName(t *testing.T, definitions []AlertDefinition, name string) AlertDefinition {
	t.Helper()
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("definition %q not found", name)
	return AlertDefinition{}
}
