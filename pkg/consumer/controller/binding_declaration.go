package controller

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/binding"
)

// DeclareBindings states the group's full binding set -- no patterns = the
// whole topic. declaredAt is when this declarer first stated the set and
// stays fixed across its retries; callers retry on DeclarationWaiting.
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

// normalizePatterns sorts and deduplicates -- the datastore compares sets
// element-wise. Always returns a non-nil slice; an empty set is a stored
// value, not NULL.
func normalizePatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	normalized = append(normalized, patterns...)
	slices.Sort(normalized)
	return slices.Compact(normalized)
}
