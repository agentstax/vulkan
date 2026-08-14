package controller

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/binding"
	"github.com/agentstax/vulkan/pkg/consumer/controller/datastore"
)

// DeclareBindings states the group's full binding set -- no patterns = the
// whole topic, '*' in a pattern matches any run of characters.
// declaredAt is when the declarer first stated the set, fixed across its
// retries; callers retry on DeclarationWaiting.
func (c *ConsumerController) DeclareBindings(ctx context.Context, groupId int64, patterns []string, declaredAt time.Time) (binding.DeclarationOutcome, error) {
	if groupId <= 0 {
		return "", errors.New("groupId must be > 0")
	}
	if declaredAt.IsZero() {
		return "", errors.New("declaredAt is required")
	}
	if slices.Contains(patterns, "") {
		return "", errors.New("patterns must not contain an empty pattern")
	}

	declared := normalizePatterns(patterns)
	return c.datastore.DeclareBindings(ctx, groupId, declared, common.ProcessIdentity, declaredAt)
}

// ListDeclarations returns every group's effective declaration followed by
// its still-waiting declarers, ordered by topic then group.
func (c *ConsumerController) ListDeclarations(ctx context.Context) ([]*binding.Declaration, error) {
	data, err := c.datastore.ListBindingDeclarations(ctx)
	if err != nil {
		return nil, err
	}

	var declarations []*binding.Declaration
	for _, rows := range groupByConsumerGroup(data) {
		effective, found := newestInstalled(rows)
		if !found {
			continue
		}
		declarations = append(declarations, toDeclaration(effective))
		for _, waiter := range openWaiters(rows, effective) {
			declarations = append(declarations, toDeclaration(waiter))
		}
	}

	// stable keeps each group's effective row ahead of its waiters
	slices.SortStableFunc(declarations, compareDeclarations)
	return declarations, nil
}

// normalizePatterns sorts and deduplicates -- the datastore compares sets
// element-wise. Always returns a non-nil slice; an empty set is a stored
// value, not NULL.
func normalizePatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	normalized = append(normalized, patterns...)
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

func groupByConsumerGroup(rows []datastore.BindingDeclarationData) map[int64][]datastore.BindingDeclarationData {
	groups := make(map[int64][]datastore.BindingDeclarationData)
	for _, row := range rows {
		groups[row.ConsumerGroupId] = append(groups[row.ConsumerGroupId], row)
	}
	return groups
}

// newestInstalled picks the group's highest-id installed row -- the effective
// declaration.
func newestInstalled(rows []datastore.BindingDeclarationData) (*datastore.BindingDeclarationData, bool) {
	var newest *datastore.BindingDeclarationData
	for i := range rows {
		if rows[i].Status != datastore.BindingDeclarationInstalled {
			continue
		}
		if newest == nil || rows[i].Id > newest.Id {
			newest = &rows[i]
		}
	}
	return newest, newest != nil
}

// openWaiters finds the waiting rows whose declarer is still blocked.
func openWaiters(rows []datastore.BindingDeclarationData, effective *datastore.BindingDeclarationData) []*datastore.BindingDeclarationData {
	var waiters []*datastore.BindingDeclarationData
	for i := range rows {
		row := &rows[i]
		if row.Status != datastore.BindingDeclarationWaiting {
			continue
		}
		if declarerInstalledAfter(row, rows) {
			continue
		}
		if slices.Equal(row.Patterns, effective.Patterns) {
			continue
		}
		waiters = append(waiters, row)
	}
	return waiters
}

// declarerInstalledAfter reports whether the waiting row's declarer went on
// to install -- an ended wait leaves its waiting row behind.
func declarerInstalledAfter(waiting *datastore.BindingDeclarationData, rows []datastore.BindingDeclarationData) bool {
	for i := range rows {
		if rows[i].Status == datastore.BindingDeclarationInstalled &&
			rows[i].DeclaredBy == waiting.DeclaredBy &&
			rows[i].Id > waiting.Id {
			return true
		}
	}
	return false
}

func compareDeclarations(a *binding.Declaration, b *binding.Declaration) int {
	if c := strings.Compare(a.TopicName, b.TopicName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.SchemaVersion, b.SchemaVersion); c != 0 {
		return c
	}
	return strings.Compare(a.GroupName, b.GroupName)
}
