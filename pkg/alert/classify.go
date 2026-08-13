package alert

import (
	"time"

	"github.com/agentstax/vulkan/pkg/producer"
)

// classify compares the alert a handler built this run (nil when it built none)
// against the compaction head it last published, and returns what to publish now.
func classify(alert *Alert, head *producer.MessageRow[Alert], repeat time.Duration, now time.Time) (*Alert, error) {
	activeHead := head != nil && head.Message.Status == StatusActive

	if alert != nil {
		switch {
		// new alert, head missing or resolved             -> publish new alert
		case !activeHead:
			return alert, nil
		// alert severity has changed                      -> publish new alert
		case alert.Severity != head.Message.Severity:
			return alert, nil
		// alert hasn't changed but repeat interval passed -> publish new alert
		case now.Sub(head.CreatedAt) >= repeat:
			return alert, nil
		default:
			return nil, nil
		}
	}

	// no new alert, head (previous) still active        -> publish the head resolved
	if activeHead {
		return NewAlert(head.Message.Name, head.Message.Owner, StatusResolved, head.Message.Severity, "resolved: "+head.Message.Message, nil)
	}
	return nil, nil
}

// statusChanged treats a missing head as a change -- the first publish logs.
func statusChanged(published *Alert, head *producer.MessageRow[Alert]) bool {
	if head == nil {
		return true
	}
	return published.Status != head.Message.Status
}
