package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/spf13/cobra"
)

func newTopicConfigCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and change a registered topic's config",
		Long: `Each config key has a default. set writes a value; unset returns the key
to its default -- the value an unconfigured register gets, not the value
the topic was registered with.

Running producers and consumers snapshot topic config at their Register,
so a change takes effect on their next restart, not live. Register calls
still passing the old config fail with a config mismatch rather than
silently reverting the change.`,
	}

	cmd.AddCommand(newTopicConfigGetCmd(g))
	cmd.AddCommand(newTopicConfigSetCmd(g))
	cmd.AddCommand(newTopicConfigUnsetCmd(g))

	return cmd
}

// topicConfigKey is one key of a topic's config: the column name config get
// prints and set/unset accept, its value shape for help and errors, how each
// verb lands on the patch, and how get reads it off a topic.
type topicConfigKey struct {
	key   string
	value string
	set   func(cfg *topiccontroller.AlterTopicConfig, raw string) error
	unset func(cfg *topiccontroller.AlterTopicConfig)
	read  func(t *topic.Topic) string
}

// topicConfigKeys holds the alterable columns. PartitionSize is absent --
// register-time only, shown by topic get.
var topicConfigKeys = []topicConfigKey{
	{
		key:   "retention_ttl",
		value: "duration, e.g. 720h (0 keeps messages forever)",
		set: func(cfg *topiccontroller.AlterTopicConfig, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			cfg.RetentionTTL = common.Set(duration)
			return nil
		},
		unset: func(cfg *topiccontroller.AlterTopicConfig) {
			cfg.RetentionTTL = common.Unset[time.Duration]()
		},
		read: func(t *topic.Topic) string {
			return retentionDetail(t.RetentionTTL)
		},
	},
	{
		key:   "allow_drop_past_committed",
		value: "true or false",
		set: func(cfg *topiccontroller.AlterTopicConfig, raw string) error {
			allowed, err := strconv.ParseBool(raw)
			if err != nil {
				return err
			}
			cfg.AllowDropPastCommitted = common.Set(allowed)
			return nil
		},
		unset: func(cfg *topiccontroller.AlterTopicConfig) {
			cfg.AllowDropPastCommitted = common.Unset[bool]()
		},
		read: func(t *topic.Topic) string {
			return strconv.FormatBool(t.AllowDropPastCommitted)
		},
	},
	{
		key:   "idempotency_key_ttl",
		value: "duration, e.g. 1h",
		set: func(cfg *topiccontroller.AlterTopicConfig, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			cfg.IdempotencyKeyTTL = common.Set(duration)
			return nil
		},
		unset: func(cfg *topiccontroller.AlterTopicConfig) {
			cfg.IdempotencyKeyTTL = common.Unset[time.Duration]()
		},
		read: func(t *topic.Topic) string {
			return t.IdempotencyKeyTTL.String()
		},
	},
	{
		key:   "delivery_log_mode",
		value: "off, failures, or all",
		set: func(cfg *topiccontroller.AlterTopicConfig, raw string) error {
			cfg.DeliveryLogMode = common.Set(topic.DeliveryLogMode(raw))
			return nil
		},
		unset: func(cfg *topiccontroller.AlterTopicConfig) {
			cfg.DeliveryLogMode = common.Unset[topic.DeliveryLogMode]()
		},
		read: func(t *topic.Topic) string {
			return string(t.DeliveryLogMode)
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

func printTopicConfigLines(w io.Writer, t *topic.Topic, entries []topicConfigKey) {
	defaults := (&topiccontroller.TopicConfig{}).WithDefaults().ToTopic(0, 0, "", 1)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  KEY\tDEFAULT\tVALUE")
	for _, entry := range entries {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", entry.key, entry.read(defaults), entry.read(t))
	}
	tw.Flush()
}

// retentionDetail is the retention cell: raw Go duration string, plus a day
// parenthetical when it's whole days ("720h0m0s (30d)"); "forever" for
// keep-indefinitely.
func retentionDetail(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	const day = 24 * time.Hour
	if d%day == 0 {
		return fmt.Sprintf("%s (%dd)", d, d/day)
	}
	return d.String()
}
