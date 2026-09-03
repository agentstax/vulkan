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

	rows := make([]map[string]any, 0, len(unclaimed))
	for _, snapshot := range unclaimed {
		rows = append(rows, map[string]any{
			"worker":           snapshot.Name,
			"owner":            snapshot.Owner.Name,
			"owner_kind":       string(snapshot.Owner.Kind()),
			"target_instances": snapshot.TargetInstances,
		})
	}

	message := fmt.Sprintf("topic %q has no live instance on %d of its worker rows", owner.Name, len(unclaimed))
	detail := fmt.Sprintf("Nothing is running: %s. A worker row with no live instance does no work: expired partitions are not dropped, exceptions are not retried, and the group's cursor stops advancing.", unclaimedByOwner(unclaimed))
	hint := "Run \"vulkan manager run\" in a process that stays up, or start a consumer on the topic -- either one claims these rows."
	data := map[string]any{
		"unclaimed_count": len(unclaimed),
		"workers":         rows,
	}
	return alert.NewAlert(AlertWorkerLiveness, owner, alert.AlertStatusActive, alert.AlertSeverityWarn, message, &alert.AlertOptions{
		Detail: detail,
		Hint:   hint,
		Data:   data,
	})
}

// unclaimedByOwner renders the rows as "<owner> (<worker>, <worker>)" so one
// dark consumer group reads as one entry, not four.
func unclaimedByOwner(unclaimed []metrics.WorkerSnapshot) string {
	owners := make([]string, 0, len(unclaimed))
	workers := map[string][]string{}
	for _, snapshot := range unclaimed {
		if _, seen := workers[snapshot.Owner.Name]; !seen {
			owners = append(owners, snapshot.Owner.Name)
		}
		workers[snapshot.Owner.Name] = append(workers[snapshot.Owner.Name], snapshot.Name)
	}

	entries := make([]string, 0, len(owners))
	for _, name := range owners {
		entries = append(entries, fmt.Sprintf("%s (%s)", name, strings.Join(workers[name], ", ")))
	}
	return strings.Join(entries, ", ")
}
