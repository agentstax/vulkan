package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// commaInt renders 1000000 as "1,000,000". Only PartitionSize gets grouping;
// everything else (batch sizes, ids) prints bare, matching ADMIN_CLI.md.
func commaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := ""
	if len(s) > 0 && s[0] == '-' {
		neg, s = "-", s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return neg + string(out)
}

// compactDuration is the list-column form: "720h", "1h", "5s" -- collapse to the
// coarsest whole unit, fall back to Go's duration string for odd values.
func compactDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0s"
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// dayParenthetical adds " (30d)" when a duration is a whole number of days.
func dayParenthetical(d time.Duration) string {
	const day = 24 * time.Hour
	if d > 0 && d%day == 0 {
		return fmt.Sprintf(" (%dd)", d/day)
	}
	return ""
}

// retentionCell is the list RETENTION column: "forever" for keep-indefinitely,
// else the compact form plus a day parenthetical.
func retentionCell(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	return compactDuration(d) + dayParenthetical(d)
}

// retentionDetail is the get form: raw Go duration string ("720h0m0s"), or
// "forever" for keep-indefinitely.
func retentionDetail(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	return d.String()
}

// timeCell renders a topic timestamp (created/updated) for the list/get views,
// to the minute, in whatever zone the driver returns it in.
func timeCell(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// topicJSON is the machine-readable shape for --json. Durations are strings
// (Go duration syntax) -- readable and unambiguous for scripts.
type topicJSON struct {
	Name                   string `json:"name"`
	ID                     int64  `json:"id"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
	PartitionSize          int64  `json:"partition_size"`
	RetentionTTL           string `json:"retention_ttl"`
	AllowDropPastCommitted bool   `json:"allow_drop_past_committed"`
	IdempotencyKeyTTL      string `json:"idempotency_key_ttl"`
	DisableDeliveryLog     bool   `json:"disable_delivery_log"`
	JanitorPollRate        string `json:"janitor_poll_rate"`
	JanitorSweepBatchSize  int    `json:"janitor_sweep_batch_size"`
}

func toTopicJSON(t *topic.Topic) topicJSON {
	return topicJSON{
		Name:                   t.Name,
		ID:                     t.Id,
		CreatedAt:              t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:              t.UpdatedAt.Format(time.RFC3339),
		PartitionSize:          t.PartitionSize,
		RetentionTTL:           t.RetentionTTL.String(),
		AllowDropPastCommitted: t.AllowDropPastCommitted,
		IdempotencyKeyTTL:      t.IdempotencyKeyTTL.String(),
		DisableDeliveryLog:     t.DisableDeliveryLog,
		JanitorPollRate:        t.JanitorPollRate.String(),
		JanitorSweepBatchSize:  t.JanitorSweepBatchSize,
	}
}

func toTopicsJSON(topics []*topic.Topic) []topicJSON {
	rows := make([]topicJSON, 0, len(topics))
	for _, t := range topics {
		rows = append(rows, toTopicJSON(t))
	}
	return rows
}

// printJSON is the single --json emission path every command shares, so
// indentation and the encode-failure message never drift between them.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return failOp("could not encode JSON: %v", err)
	}
	return nil
}

// pluralize - "1 topic" / "2 topics".
func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
