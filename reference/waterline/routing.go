package waterline

import (
	"context"
	"fmt"
	"strings"
)

// Routing (Phase 7). Producers publish with attributes (topic / routing_key /
// headers); bindings decide who receives. The decision is a PREDICATE EVALUATED
// AT FAN-OUT TIME — here, that is read/claim time (readRange in pglog.go pushes
// the binding predicate into SQL). A group with NO binding matches all events.
//
// Design choice (hybrid-consistent): routing does NOT materialize a per-event
// row on the happy path. The cursor still advances over the whole contiguous
// block, so `committed` stays a dense frontier; an offset a group is not bound
// to simply produces no work and no exception row (it is "resolved" the instant
// the block commits). The throughput win of claim-from-log is preserved.
//
// Because the cursor advances over the whole log, a binding added AFTER events
// exist only affects offsets at/above the group's current frontier. To route
// historical events to a new binding, replay (reset the cursor) — exactly the
// Phase 7 "what changes if a binding is added after events exist?" answer.

// BindTopic adds a NATS-style topic binding for a group. The pattern uses `.`
// token separators, `*` (one token), and `>` (one-or-more trailing tokens); it
// is matched against events.routing_key. e.g. "orders.*.created", "orders.us.>".
func (l *PgLog) BindTopic(ctx context.Context, group, pattern string) error {
	rx, err := natsToRegex(pattern)
	if err != nil {
		return err
	}
	_, err = l.Pool.Exec(ctx,
		`INSERT INTO bindings(consumer_group, kind, pattern, display) VALUES ($1,'topic',$2,$3)`,
		group, rx, pattern)
	return err
}

// BindHeader adds a header/content binding for a group. matchJSON is a JSON
// object; an event matches when events.headers @> matchJSON (JSONB containment).
// e.g. `{"region":"eu","tier":"gold"}`. An empty object is REJECTED: `@> '{}'`
// is true for every event, so it would silently broaden the group to match all
// events and defeat any other binding (a privilege foot-gun).
func (l *PgLog) BindHeader(ctx context.Context, group, matchJSON string) error {
	if t := strings.TrimSpace(matchJSON); t == "" || t == "{}" {
		return fmt.Errorf("waterline: empty header match would match ALL events; use a non-empty object")
	}
	_, err := l.Pool.Exec(ctx,
		`INSERT INTO bindings(consumer_group, kind, header_match) VALUES ($1,'header',$2::jsonb)`,
		group, matchJSON)
	return err
}

// ClearBindings removes all bindings for a group (-> matches all events again).
func (l *PgLog) ClearBindings(ctx context.Context, group string) error {
	_, err := l.Pool.Exec(ctx, `DELETE FROM bindings WHERE consumer_group=$1`, group)
	return err
}

// natsToRegex translates a NATS subject pattern into an anchored POSIX regex
// suitable for the `~` operator. `*` -> one token ([^.]+); `>` -> one-or-more
// trailing tokens (.+, must be the final token); literals are regex-escaped.
func natsToRegex(pattern string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("waterline: empty topic pattern")
	}
	tokens := strings.Split(pattern, ".")
	var b strings.Builder
	b.WriteByte('^')
	for i, tok := range tokens {
		if i > 0 {
			b.WriteString(`\.`)
		}
		switch tok {
		case "":
			// empty token (e.g. "orders..created", leading/trailing dot) can never
			// match a well-formed routing key — reject rather than route zero events.
			return "", fmt.Errorf("waterline: empty token in topic pattern %q", pattern)
		case "*":
			b.WriteString(`[^.]+`)
		case ">":
			if i != len(tokens)-1 {
				return "", fmt.Errorf("waterline: `>` must be the last token in %q", pattern)
			}
			// already wrote the `\.` separator above; match the rest greedily.
			b.WriteString(`.+`)
		default:
			b.WriteString(regexEscape(tok))
		}
	}
	b.WriteByte('$')
	return b.String(), nil
}

// regexEscape escapes POSIX regex metacharacters in a literal subject token.
func regexEscape(s string) string {
	const meta = `\.+*?()|[]{}^$`
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(meta, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
