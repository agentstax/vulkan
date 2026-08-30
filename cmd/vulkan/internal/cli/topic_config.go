package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/spf13/cobra"
)

func newTopicConfigCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read a registered topic's config",
		Long: `Topic config comes from the cfg your code passes to RegisterTopic, and
is applied every time that runs. Changing a value means changing that code
and redeploying; this command only reads.

Running producers and consumers read topic config once, at their Register,
so a change takes effect on their next restart, not live.`,
	}

	cmd.AddCommand(newTopicConfigGetCmd(g))

	return cmd
}

// topicConfigKey is one key of a topic's config: the column name config get
// prints, its value shape for help and errors, and how get reads it off a
// topic.
type topicConfigKey struct {
	key   string
	value string
	read  func(found *topic.Topic) string
}

// topicConfigKeys holds the keys config get prints. PartitionSize is absent --
// it is fixed at creation and shown by topic get.
var topicConfigKeys = []topicConfigKey{
	{
		key:   "retention_ttl",
		value: "duration, e.g. 720h (0 keeps messages forever)",
		read: func(found *topic.Topic) string {
			return retentionDetail(found.RetentionTTL)
		},
	},
	{
		key:   "allow_drop_past_committed",
		value: "true or false",
		read: func(found *topic.Topic) string {
			return strconv.FormatBool(found.AllowDropPastCommitted)
		},
	},
	{
		key:   "idempotency_key_ttl",
		value: "duration, e.g. 1h",
		read: func(found *topic.Topic) string {
			return found.IdempotencyKeyTTL.String()
		},
	},
	{
		key:   "delivery_log_mode",
		value: "off, failures, or all",
		read: func(found *topic.Topic) string {
			return string(found.DeliveryLogMode)
		},
	},
}

func findTopicConfigKey(key string) (topicConfigKey, bool) {
	for _, entry := range topicConfigKeys {
		if entry.key == key {
			return entry, true
		}
	}
	return topicConfigKey{}, false
}

// errUnknownTopicConfigKey rejects a key the table doesn't know, listing the
// vocabulary.
func errUnknownTopicConfigKey(key string) error {
	var known []string
	for _, entry := range topicConfigKeys {
		known = append(known, fmt.Sprintf("  %-26s %s", entry.key, entry.value))
	}
	return failUsage("unknown config key %q -- known keys:\n%s", key, strings.Join(known, "\n"))
}

// topicConfigDocument is topic config get's json result: each key with its
// compiled-in default and the topic's current value, as the table renders
// them.
type topicConfigDocument struct {
	Topic   string                   `json:"topic"`
	TopicId int64                    `json:"topic_id"`
	Keys    []topicConfigKeyDocument `json:"keys"`
}

type topicConfigKeyDocument struct {
	Key     string `json:"key"`
	Default string `json:"default"`
	Value   string `json:"value"`
}

func toTopicConfigDocument(found *topic.Topic, entries []topicConfigKey) topicConfigDocument {
	defaults := (&topiccontroller.TopicConfig{}).WithDefaults().ToTopic(0, 0, "")

	keys := make([]topicConfigKeyDocument, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, topicConfigKeyDocument{
			Key:     entry.key,
			Default: entry.read(defaults),
			Value:   entry.read(found),
		})
	}
	return topicConfigDocument{
		Topic:   found.Name,
		TopicId: found.Id,
		Keys:    keys,
	}
}

func printTopicConfigLines(w io.Writer, found *topic.Topic, entries []topicConfigKey) {
	defaults := (&topiccontroller.TopicConfig{}).WithDefaults().ToTopic(0, 0, "")
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  KEY\tDEFAULT\tVALUE")
	for _, entry := range entries {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", entry.key, entry.read(defaults), entry.read(found))
	}
	tw.Flush()
}

// retentionDetail is the retention cell: raw Go duration string, plus a day
// parenthetical when it's whole days ("720h0m0s (30d)"); "forever" for
// keep-indefinitely.
func retentionDetail(retention time.Duration) string {
	if retention == 0 {
		return "forever"
	}
	const day = 24 * time.Hour
	if retention%day == 0 {
		return fmt.Sprintf("%s (%dd)", retention, retention/day)
	}
	return retention.String()
}
