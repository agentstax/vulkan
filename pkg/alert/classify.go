package alert

import (
	"time"

	"github.com/agentstax/vulkan/pkg/producer"
)

// classify compares the alert a check built this run (nil when it built none)
// against the compaction head it last published, and returns what to publish now.
func classify(alert *Alert, head *producer.MessageRow[Alert], repeat time.Duration, now time.Time) *Alert {
	activeHead := head != nil && head.Message.Status == StatusActive

	if alert != nil {
		switch {
		// new alert, head missing or resolved             -> publish new alert
		case !activeHead:
			return alert
		// alert severity has changed                      -> publish new alert
		case alert.Severity != head.Message.Severity:
			return alert
		// alert hasn't changed but repeat interval passed -> publish new alert
		case now.Sub(head.CreatedAt) >= repeat:
			return alert
		default:
			return nil
		}
	}

	// no alert, head still active                       -> return head but with resolved status
	if activeHead {
		resolved := *head.Message
		resolved.Status = StatusResolved
		return &resolved
	}
	return nil
}

// statusChanged reports whether the alert being published has a different
// status than the head it replaces.
func statusChanged(published *Alert, head *producer.MessageRow[Alert]) bool {
	if head == nil {
		return true
	}
	return published.Status != head.Message.Status
}
