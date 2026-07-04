package consumer

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// scopes a group's routing to events whose routing_key matches a wildcard
// pattern ('*' matches any run of characters, any depth -- e.g.
// "orders.*.created" also matches "orders.us.central1.created"); translated
// here to a POSIX regex for the claim/fan-out predicate's '~' match. A group
// with no binding at all matches every event.
//
// TODO - this is a true wildcard, not a NATS-style selector -- it can't pin an
// exact token depth (see TODO.md).
func (d *PostgresDatastore[Message]) BindTopic(ctx context.Context, consumerGroup, pattern string) error {
	rx, err := wildcardToRegex(pattern)
	if err != nil {
		return err
	}

	sql := `
		INSERT INTO bindings (consumer_group, pattern, display)
		VALUES ($1, $2, $3);
	`

	_, err = d.Pool.Exec(ctx, sql, consumerGroup, rx, pattern)
	return err
}

// removes every binding for a group -> it goes back to matching all events.
func (d *PostgresDatastore[Message]) ClearBindings(ctx context.Context, consumerGroup string) error {
	sql := `
		DELETE FROM bindings
		WHERE consumer_group = $1;
	`

	_, err := d.Pool.Exec(ctx, sql, consumerGroup)
	return err
}

// translates a '*'-wildcard pattern into an anchored POSIX regex suitable for
// the `~` operator: '*' -> `.*` (any characters, unbounded), literal segments
// regex-escaped.
func wildcardToRegex(pattern string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("consumer: empty topic pattern")
	}

	segments := strings.Split(pattern, "*")
	var b strings.Builder
	b.WriteByte('^')
	for i, seg := range segments {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(seg))
	}
	b.WriteByte('$')
	return b.String(), nil
}
