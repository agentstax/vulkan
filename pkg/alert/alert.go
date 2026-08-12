package alert

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// Status is an alert's lifecycle state on its compaction key -- an active
// alert and its later resolution are versions of the same key.
type Status string

const (
	StatusActive   Status = "active"
	StatusResolved Status = "resolved"
)

// Severity is the alert's urgency; only "warn" is accepted today.
type Severity string

const SeverityWarn Severity = "warn"

// Alert is one check finding, published to the __system.alerts topic as an
// ordinary message.
type Alert struct {
	// identity
	Name     string        // the check, e.g. "partition_count"
	Owner    *common.Owner // the resource the alert is about
	Status   Status        // "active" | "resolved"
	Severity Severity      // "warn"

	// prose -- Postgres MESSAGE/DETAIL/HINT
	Message string
	Detail  string
	Hint    string

	// evidence. GUARDRAIL: neither map ever routes, keys, or dedups.
	Data     map[string]any // the check's measurements
	Metadata map[string]any // context about the report (evaluator, first_active_at, repeat count)
}

func NewAlert(name string, owner *common.Owner, status Status, severity Severity, message string, detail string, hint string, data map[string]any, metadata map[string]any) (*Alert, error) {
	if name == "" {
		return nil, fmt.Errorf("alert name is required")
	}
	if owner == nil {
		return nil, fmt.Errorf("alert %q: owner is required", name)
	}
	// the routing key embeds the owner name
	if owner.Name == "" {
		return nil, fmt.Errorf("alert %q: owner name is required", name)
	}

	switch status {
	case StatusActive, StatusResolved:
	default:
		return nil, fmt.Errorf("alert %q: invalid status %q", name, status)
	}

	if severity != SeverityWarn {
		return nil, fmt.Errorf("alert %q: invalid severity %q", name, severity)
	}

	return &Alert{
		Name:     name,
		Owner:    owner,
		Status:   status,
		Severity: severity,
		Message:  message,
		Detail:   detail,
		Hint:     hint,
		Data:     data,
		Metadata: metadata,
	}, nil
}

// RoutingKey is alert.<name>.<owner-kind>.<owner-name>.<severity>. Severity is
// LAST so a suffix binding can match on it; kind precedes name so names can't
// collide across kinds. The ONLY place this string is composed.
func (a *Alert) RoutingKey() string {
	return fmt.Sprintf("alert.%s.%s.%s.%s", a.Name, a.Owner.Kind(), a.Owner.Name, a.Severity)
}

// CompactionKey is <name>/<owner-kind>/<owner-id>. Kind keeps id spaces from
// colliding across kinds. The ONLY place this string is composed -- a check
// builds the same key from its name to read its head even when it has nothing
// to publish.
func CompactionKey(name string, owner *common.Owner) string {
	var id int64
	switch owner.Kind() {
	case common.OwnerSystem:
		id = owner.SystemId
	case common.OwnerTopic:
		id = owner.TopicId
	case common.OwnerConsumerGroup:
		id = owner.ConsumerGroupId
	}
	return fmt.Sprintf("%s/%s/%d", name, owner.Kind(), id)
}

func (a *Alert) CompactionKey() string {
	return CompactionKey(a.Name, a.Owner)
}
