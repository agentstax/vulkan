package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/consume/controller/datastore"
)

// DeclareBindings states the group's full binding set -- no patterns = the
// whole topic, '*' in a pattern matches any run of characters.
// declaredAt is when the declarer first stated the set, fixed across its
// retries; callers retry on BindingWaiting.
func (c *ConsumeController) DeclareBindings(ctx context.Context, topicId int64, groupId int64, patterns []string, declaredAt time.Time) (consume.BindingOutcome, error) {
	if topicId <= 0 {
		return "", fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return "", fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if declaredAt.IsZero() {
		return "", errors.New("declaredAt is required")
	}
	if slices.Contains(patterns, "") {
		return "", errors.New("patterns must not contain an empty pattern")
	}

	declared := normalizePatterns(patterns)
	return c.datastore.DeclareBindings(ctx, topicId, groupId, declared, common.ProcessIdentity, declaredAt)
}

// ListBindingDeclarations returns every group's effective declaration followed by
// its still-waiting declarers, ordered by topic then group.
func (c *ConsumeController) ListBindingDeclarations(ctx context.Context) ([]*consume.BindingDeclaration, error) {
	data, err := c.datastore.ListBindingLog(ctx)
	if err != nil {
		return nil, err
	}

	var declarations []*consume.BindingDeclaration
	for _, rows := range groupByConsumerGroup(data) {
		effective, found := datastore.NewestInstalledDeclaration(rows)
		if !found {
			continue
		}
		declarations = append(declarations, toBindingDeclaration(effective))
		for _, waiter := range openWaiters(rows, effective) {
			declarations = append(declarations, toBindingDeclaration(&waiter))
		}
	}

	// stable keeps each group's effective row ahead of its waiters
	slices.SortStableFunc(declarations, compareBindingDeclarations)
	return declarations, nil
}

// ***************
// *** HELPERS ***
// ***************

// normalizePatterns sorts and deduplicates -- the datastore compares sets
// element-wise. Always returns a non-nil slice; an empty set is a stored
// value, not NULL.
func normalizePatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	normalized = append(normalized, patterns...)
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

func groupByConsumerGroup(rows []datastore.BindingConfigLogRow) map[int64][]datastore.BindingConfigLogRow {
	groups := make(map[int64][]datastore.BindingConfigLogRow)
	for _, row := range rows {
		groups[row.ConsumerGroupId] = append(groups[row.ConsumerGroupId], row)
	}
	return groups
}

// openWaiters finds the waiting rows whose declarer is still blocked.
func openWaiters(rows []datastore.BindingConfigLogRow, effective *datastore.BindingConfigLogRow) []datastore.BindingConfigLogRow {
	var waiters []datastore.BindingConfigLogRow
	for i := range rows {
		row := &rows[i]
		if row.Status != datastore.BindingLogWaiting {
			continue
		}
		if declarerInstalledAfter(row, rows) {
			continue
		}
		if slices.Equal(row.Patterns, effective.Patterns) {
			continue
		}
		waiters = append(waiters, rows[i])
	}
	return waiters
}

// declarerInstalledAfter reports whether the waiting row's declarer went on
// to install -- an ended wait leaves its waiting row behind.
func declarerInstalledAfter(waiting *datastore.BindingConfigLogRow, rows []datastore.BindingConfigLogRow) bool {
	for i := range rows {
		if rows[i].Status == datastore.BindingLogInstalled &&
			rows[i].DeclaredBy == waiting.DeclaredBy &&
			rows[i].Id > waiting.Id {
			return true
		}
	}
	return false
}

func compareBindingDeclarations(left *consume.BindingDeclaration, right *consume.BindingDeclaration) int {
	if c := strings.Compare(left.TopicName, right.TopicName); c != 0 {
		return c
	}
	return strings.Compare(left.GroupName, right.GroupName)
}
