package alert

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// Status is an alert's lifecycle state -- an active alert and its later
// resolution are versions of one compacted message key.
type AlertStatus string

const (
	AlertStatusActive   AlertStatus = "active"
	AlertStatusResolved AlertStatus = "resolved"
)

type AlertSeverity string

const AlertSeverityWarn AlertSeverity = "warn"

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
	Name     string        `json:"name"`  // e.g. "partition_count"
	Owner    *common.Owner `json:"owner"` // the resource the alert is about
	Status   AlertStatus   `json:"status"`
	Severity AlertSeverity `json:"severity"`

	// prose -- Postgres MESSAGE/DETAIL/HINT
	Message string `json:"message"`
	Detail  string `json:"detail"`
	Hint    string `json:"hint"`

	// evidence -- neither map ever routes, keys, or dedups
	Data     map[string]any `json:"data"`     // the run's measurements of the owner
	Metadata map[string]any `json:"metadata"` // context about the report itself
}

func (Alert) SchemaVersion() int { return 1 }

// AlertOptions are NewAlert's optional fields.
type AlertOptions struct {
	Detail   string
	Hint     string
	Data     map[string]any
	Metadata map[string]any
}

func NewAlert(name string, owner *common.Owner, status AlertStatus, severity AlertSeverity, message string, options *AlertOptions) (*Alert, error) {
	if name == "" {
		return nil, errors.New("alert name is required")
	}
	if owner == nil {
		return nil, fmt.Errorf("alert %q: owner is required", name)
	}

	// the routing key embeds the owner name
	if owner.Name == "" {
		return nil, fmt.Errorf("alert %q: owner name is required", name)
	}

	switch status {
	case AlertStatusActive, AlertStatusResolved:
	default:
		return nil, fmt.Errorf("alert %q: status must be one of %q, %q, got %q", name, AlertStatusActive, AlertStatusResolved, status)
	}

	if severity != AlertSeverityWarn {
		return nil, fmt.Errorf("alert %q: severity must be %q, got %q", name, AlertSeverityWarn, severity)
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

// MessageKey is <name>/<owner-kind>/<owner-id>, composed nowhere else; kind
// keeps id spaces from colliding. A run that built no alert still builds
// this key to read its head.
func MessageKey(name string, owner *common.Owner) (string, error) {
	var id int64
	switch owner.Kind() {
	case common.OwnerSystem:
		id = owner.SystemId
	case common.OwnerTopic:
		id = owner.TopicId
	case common.OwnerConsumerGroup:
		id = owner.ConsumerGroupId
	default:
		return "", fmt.Errorf("alert %q: unrecognized owner kind: %q", name, owner.Kind())
	}
	return fmt.Sprintf("%s/%s/%d", name, owner.Kind(), id), nil
}
