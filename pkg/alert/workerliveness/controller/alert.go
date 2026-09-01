package controller

import (
	"errors"
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// The crossing decision is the caller's -- an alert built from no unclaimed
// rows is a bug.
func newWorkerLivenessAlert(owner *common.Owner, unclaimed []metrics.WorkerSnapshot) (*alert.Alert, error) {
	if len(unclaimed) == 0 {
		return nil, errors.New("unclaimed must not be empty")
	}

	named := make([]string, 0, len(unclaimed))
	rows := make([]map[string]any, 0, len(unclaimed))
	for _, snapshot := range unclaimed {
		named = append(named, fmt.Sprintf("%s (%s)", snapshot.Name, snapshot.Owner.Name))
		rows = append(rows, map[string]any{
			"worker":           snapshot.Name,
			"owner":            snapshot.Owner.Name,
			"owner_kind":       string(snapshot.Owner.Kind()),
			"target_instances": snapshot.TargetInstances,
		})
	}

	message := fmt.Sprintf("topic %q has %d worker rows with no live instance", owner.Name, len(unclaimed))
	detail := fmt.Sprintf("Nothing is running %s. A worker row with no live instance does no work: expired partitions are not dropped, exceptions are not retried, and the group's cursor stops advancing.", strings.Join(named, ", "))
	hint := "Run \"vulkan manager run\" in a process that stays up, or start a consumer on the topic -- either one claims these rows."
	data := map[string]any{
		"unclaimed_count": len(unclaimed),
		"workers":         rows,
	}
	return alert.NewAlert(AlertWorkerLiveness, owner, alert.StatusActive, alert.SeverityWarn, message, &alert.AlertOptions{
		Detail: detail,
		Hint:   hint,
		Data:   data,
	})
}
