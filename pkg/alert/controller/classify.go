package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
)

// classify compares the alert a run built (nil when it built none) against
// the compaction head it last published, and returns what to publish now.
func classify(found *alert.Alert, head *common.StoredMessage[alert.Alert], repeat time.Duration, now time.Time) (*alert.Alert, error) {
	activeHead := head != nil && head.Message.Status == alert.AlertStatusActive

	if found != nil {
		switch {
		// new alert, head missing or resolved             -> publish new alert
		case !activeHead:
			return found, nil
		// alert severity has changed                      -> publish new alert
		case found.Severity != head.Message.Severity:
			return found, nil
		// alert hasn't changed but repeat interval passed -> publish new alert
		case now.Sub(head.CreatedAt) >= repeat:
			return found, nil
		default:
			return nil, nil
		}
	}

	// no new alert, head (previous) still active        -> publish the head resolved
	if activeHead {
		return alert.NewAlert(head.Message.Name, head.Message.Owner, alert.AlertStatusResolved, head.Message.Severity, "resolved: "+head.Message.Message, now, nil)
	}
	return nil, nil
}

// statusChanged treats a missing head as a change -- the first publish logs.
func statusChanged(published *alert.Alert, head *common.StoredMessage[alert.Alert]) bool {
	if head == nil {
		return true
	}
	return published.Status != head.Message.Status
}
