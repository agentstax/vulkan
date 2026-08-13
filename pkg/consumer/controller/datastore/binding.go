package datastore

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Bind scopes a group's routing to events whose routing_key matches a wildcard
// pattern ('*' matches any run of characters, any depth -- e.g.
// "orders.*.created" also matches "orders.us.central1.created"); translated
// here to a POSIX regex for the claim/fan-out predicate's '~' match. A group
// with no binding at all matches every event. Binding a pattern the group
// already has is a no-op.
//
// Binding changes apply forward only: FanOut never revisits messages below the
// group's cursor, so history a previous binding skipped stays skipped.
//
// TODO - this is a true wildcard, not a NATS-style selector -- it can't pin an
// exact token depth.
func (d *ConsumerDatastore) Bind(ctx context.Context, groupID int64, pattern string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.bind(ctx, groupID, pattern)
	})
}

func (d *ConsumerDatastore) bind(ctx context.Context, groupID int64, pattern string) error {
	expression, err := wildcardToRegex(pattern)
	if err != nil {
		return err
	}

	sql := `
		INSERT INTO binding (consumer_group_id, pattern, display)
		VALUES ($1, $2, $3)
		ON CONFLICT (consumer_group_id, pattern) DO NOTHING;
	`

	_, err = d.Datastore.Pool.Exec(ctx, sql, groupID, expression, pattern)
	return err
}

// ClearBindings removes every binding for a group -> it goes back to matching
// all events on its topic, forward only (see Bind).
func (d *ConsumerDatastore) ClearBindings(ctx context.Context, groupID int64) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.clearBindings(ctx, groupID)
	})
}

func (d *ConsumerDatastore) clearBindings(ctx context.Context, groupID int64) error {
	sql := `
		DELETE FROM binding
		WHERE consumer_group_id = $1;
	`

	_, err := d.Datastore.Pool.Exec(ctx, sql, groupID)
	return err
}

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
