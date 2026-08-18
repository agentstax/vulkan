package alert

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// Status is an alert's lifecycle state -- an active alert and its later
// resolution are versions of one compaction key.
type Status string

const (
	StatusActive   Status = "active"
	StatusResolved Status = "resolved"
)

type Severity string

const SeverityWarn Severity = "warn"

// RecordOutcome is what one AlertController.Record call published.
type RecordOutcome string

const (
	RecordOutcomeActive   RecordOutcome = "active"   // published a new active alert
	RecordOutcomeResolved RecordOutcome = "resolved" // published the head resolved
	RecordOutcomeNothing  RecordOutcome = "nothing"  // classify chose not to publish
)

// Alert is what one run found for one owner, published to the
// __system.alerts topic as an ordinary message.
type Alert struct {
	// identity
	Name     string        // e.g. "partition_count"
	Owner    *common.Owner // the resource the alert is about
	Status   Status
	Severity Severity

	// prose -- Postgres MESSAGE/DETAIL/HINT
	Message string
	Detail  string
	Hint    string

	// evidence -- neither map ever routes, keys, or dedups
	Data     map[string]any // the run's measurements of the owner
	Metadata map[string]any // context about the report itself
}

// AlertOptions are NewAlert's optional fields.
type AlertOptions struct {
	Detail   string
	Hint     string
	Data     map[string]any
	Metadata map[string]any
}

func NewAlert(name string, owner *common.Owner, status Status, severity Severity, message string, options *AlertOptions) (*Alert, error) {
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

	if options == nil {
		options = &AlertOptions{}
	}
	return &Alert{
		Name:     name,
		Owner:    owner,
		Status:   status,
		Severity: severity,
		Message:  message,
		Detail:   options.Detail,
		Hint:     options.Hint,
		Data:     options.Data,
		Metadata: options.Metadata,
	}, nil
}

// RoutingKey is alert.<name>.<owner-kind>.<severity>.<owner-name>, composed
// nowhere else.
//   - owner name can contain dots, so it goes last -> fixed-depth prefix
//   - kind before owner name -> names can't collide across kinds
func (a *Alert) RoutingKey() string {
	return fmt.Sprintf("alert.%s.%s.%s.%s", a.Name, a.Owner.Kind(), a.Severity, a.Owner.Name)
}

// CompactionKey is <name>/<owner-kind>/<owner-id>, composed nowhere else; kind
// keeps id spaces from colliding. A run that built no alert still builds
// this key to read its head.
func CompactionKey(name string, owner *common.Owner) (string, error) {
	var id int64
	switch owner.Kind() {
	case common.OwnerSystem:
		id = owner.SystemId
	case common.OwnerTopic:
		id = owner.TopicId
	case common.OwnerConsumerGroup:
		id = owner.ConsumerGroupId
	default:
		return "", fmt.Errorf("alert %q: unhandled owner kind %q", name, owner.Kind())
	}
	return fmt.Sprintf("%s/%s/%d", name, owner.Kind(), id), nil
}
