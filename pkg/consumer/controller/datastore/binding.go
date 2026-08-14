package datastore

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ListBindings reads every binding row with its group and topic names.
func (d *ConsumerDatastore) ListBindings(ctx context.Context) ([]BindingData, error) {
	var bindings []BindingData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		bindings, err = d.listBindings(ctx)
		return err
	})
	return bindings, err
}

func (d *ConsumerDatastore) listBindings(ctx context.Context) ([]BindingData, error) {
	sql := `
		SELECT
			consumer_group.name,
			topic.name,
			topic.schema_version,
			COALESCE(binding.display, binding.pattern)
		FROM binding
		JOIN consumer_group ON consumer_group.id = binding.consumer_group_id
		JOIN topic ON topic.id = consumer_group.topic_id
		ORDER BY topic.name, topic.schema_version, consumer_group.name, binding.id;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bindings []BindingData
	for rows.Next() {
		var binding BindingData
		if err := rows.Scan(
			&binding.GroupName,
			&binding.TopicName,
			&binding.SchemaVersion,
			&binding.Pattern,
		); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

// translates a '*'-wildcard pattern into an anchored POSIX regex suitable for
// the `~` operator: '*' -> `.*` (any characters, unbounded), literal segments
// regex-escaped.
func wildcardToRegex(pattern string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("consumer: empty topic pattern")
	}

	segments := strings.Split(pattern, "*")
	var builder strings.Builder
	builder.WriteByte('^')
	for i, segment := range segments {
		if i > 0 {
			builder.WriteString(".*")
		}
		builder.WriteString(regexp.QuoteMeta(segment))
	}
	builder.WriteByte('$')
	return builder.String(), nil
}
