package alert

import "testing"

func TestRoutingKey(t *testing.T) {
	// entity-type before the (dotted) entity name; severity is LAST
	a := &Alert{Name: "partition_count", EntityType: EntityTypeTopic, EntityName: "orders.created", Severity: SeverityWarn}
	if got, want := a.RoutingKey(), "alert.partition_count.topic.orders.created.warn"; got != want {
		t.Errorf("RoutingKey() = %q, want %q", got, want)
	}
}

func TestCompactionKey(t *testing.T) {
	a := &Alert{Name: "partition_count", EntityType: EntityTypeTopic, EntityId: 42}
	if got, want := a.CompactionKey(), "partition_count/topic/42"; got != want {
		t.Errorf("CompactionKey() = %q, want %q", got, want)
	}
}

func TestNewAlertDefaultsSystemEntityName(t *testing.T) {
	a, err := NewAlert("x", EntityTypeSystem, 0, "", StatusFiring, SeverityWarn, "m", "d", "h", nil, nil)
	if err != nil {
		t.Fatalf("NewAlert: %v", err)
	}
	if a.EntityName != SystemEntityName {
		t.Errorf("EntityName = %q, want %q", a.EntityName, SystemEntityName)
	}
	if got, want := a.RoutingKey(), "alert.x.system.system.warn"; got != want {
		t.Errorf("RoutingKey() = %q, want %q", got, want)
	}
}

func TestNewAlertRejects(t *testing.T) {
	cases := map[string]struct {
		name       string
		entityType EntityType
		entityId   int64
		entityName string
		status     Status
		severity   Severity
	}{
		"empty name":             {"", EntityTypeTopic, 5, "orders", StatusFiring, SeverityWarn},
		"invalid entity type":    {"x", "cluster", 5, "orders", StatusFiring, SeverityWarn},
		"system with nonzero id": {"x", EntityTypeSystem, 5, "", StatusFiring, SeverityWarn},
		"topic with zero id":     {"x", EntityTypeTopic, 0, "orders", StatusFiring, SeverityWarn},
		"topic missing name":     {"x", EntityTypeTopic, 5, "", StatusFiring, SeverityWarn},
		"bad status":             {"x", EntityTypeSystem, 0, "system", "bogus", SeverityWarn},
		"unpopulated severity":   {"x", EntityTypeSystem, 0, "system", StatusFiring, "critical"},
	}
	for name, c := range cases {
		if _, err := NewAlert(c.name, c.entityType, c.entityId, c.entityName, c.status, c.severity, "", "", "", nil, nil); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}
