package alert

import "fmt"

// Status is an alert's lifecycle state on its compaction key -- a firing
// alert and its later resolution are versions of the same key.
type Status string

const (
	StatusFiring   Status = "firing"
	StatusResolved Status = "resolved"
)

// Severity is the alert's urgency; only "warn" is accepted today.
type Severity string

const SeverityWarn Severity = "warn"

// EntityType is what an alert is about. Paired with EntityId (a rename-proof
// machine id) it names the subject; consumer groups are NOT addressable this
// way, having no such id.
type EntityType string

const (
	EntityTypeSystem EntityType = "system"
	EntityTypeTopic  EntityType = "topic"
)

// SystemEntityName is the EntityName an EntityTypeSystem alert takes when none
// is given, so its routing key is deterministic.
const SystemEntityName = "system"

// Alert is one check finding, published to the __system.alerts topic as an
// ordinary message.
type Alert struct {
	// identity
	Name       string     // the check, e.g. "partition_count"
	EntityType EntityType // "system" | "topic"
	EntityId   int64      // rename-proof machine id; 0 = system
	EntityName string     // human handle -- the topic name, or SystemEntityName
	Status     Status     // "firing" | "resolved"
	Severity   Severity   // "warn"

	// prose -- Postgres MESSAGE/DETAIL/HINT
	Message string
	Detail  string
	Hint    string

	// evidence. GUARDRAIL: neither map ever routes, keys, or dedups.
	Data     map[string]any // measurements about the entity (the check's evidence)
	Metadata map[string]any // context about the report (evaluator, first_fired_at, repeat count)
}

func NewAlert(name string, entityType EntityType, entityId int64, entityName string, status Status, severity Severity, message, detail, hint string, data, metadata map[string]any) (*Alert, error) {
	if name == "" {
		return nil, fmt.Errorf("alert name is required")
	}

	switch entityType {
	case EntityTypeSystem:
		// system is the id-0 singleton; it may omit the name and take the pinned one
		if entityId != 0 {
			return nil, fmt.Errorf("alert %q: system entity must have id 0, got %d", name, entityId)
		}
		if entityName == "" {
			entityName = SystemEntityName
		}
	case EntityTypeTopic:
		if entityId == 0 {
			return nil, fmt.Errorf("alert %q: topic entity requires a non-zero id", name)
		}
		if entityName == "" {
			return nil, fmt.Errorf("alert %q: EntityName is required for topic %d", name, entityId)
		}
	default:
		return nil, fmt.Errorf("alert %q: invalid entity type %q", name, entityType)
	}

	switch status {
	case StatusFiring, StatusResolved:
	default:
		return nil, fmt.Errorf("alert %q: invalid status %q", name, status)
	}

	if severity != SeverityWarn {
		return nil, fmt.Errorf("alert %q: invalid severity %q", name, severity)
	}

	return &Alert{
		Name:       name,
		EntityType: entityType,
		EntityId:   entityId,
		EntityName: entityName,
		Status:     status,
		Severity:   severity,
		Message:    message,
		Detail:     detail,
		Hint:       hint,
		Data:       data,
		Metadata:   metadata,
	}, nil
}

// RoutingKey is alert.<name>.<entity-type>.<entity-name>.<severity>. Severity is
// LAST so a suffix binding can match on it; entity-type precedes entity-name so
// names can't collide across types. The ONLY place this string is composed.
func (a *Alert) RoutingKey() string {
	return fmt.Sprintf("alert.%s.%s.%s.%s", a.Name, a.EntityType, a.EntityName, a.Severity)
}

// CompactionKey is <name>/<entity-type>/<entity-id>. Entity-type keeps id spaces
// from colliding across types. The ONLY place this string is composed -- a check
// builds the same key from its name to read its head even when nothing fires.
func CompactionKey(name string, entityType EntityType, entityId int64) string {
	return fmt.Sprintf("%s/%s/%d", name, entityType, entityId)
}

func (a *Alert) CompactionKey() string {
	return CompactionKey(a.Name, a.EntityType, a.EntityId)
}
