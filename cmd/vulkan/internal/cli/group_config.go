package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/spf13/cobra"
)

func newGroupConfigCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and change a consumer group's config",
		Long: `A group's config is two layers: the defaults its code declares when it
starts consuming, and operator overrides. The effective value is the
override if set, else the default.

set writes an override; unset removes it, returning the key to its
default. Overrides survive redeploys until unset and take effect when the
group next claims work, not live.`,
	}

	cmd.AddCommand(newGroupConfigGetCmd(g))
	cmd.AddCommand(newGroupConfigSetCmd(g))
	cmd.AddCommand(newGroupConfigUnsetCmd(g))

	return cmd
}

// groupConfigKey is one key of the group's config: the name config get
// prints and set/unset accept, its value shape for help and errors, and how
// each verb lands on the patch.
type groupConfigKey struct {
	key   string
	value string
	set   func(cfg *admin.AlterGroupConfig, raw string) error
	unset func(cfg *admin.AlterGroupConfig)
}

// groupConfigKeys holds the keys set and unset one value at a time. The
// message document is not here -- it sets per field (messageFieldKeys) and
// unsets whole.
var groupConfigKeys = []groupConfigKey{
	{
		key:   "claim_poll_rate",
		value: "duration, e.g. 1s",
		set: func(cfg *admin.AlterGroupConfig, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			cfg.ClaimPollRate = common.Set(duration)
			return nil
		},
		unset: func(cfg *admin.AlterGroupConfig) {
			cfg.ClaimPollRate = common.Unset[time.Duration]()
		},
	},
	{
		key:   "max_range_reclaims",
		value: "count, e.g. 3",
		set: func(cfg *admin.AlterGroupConfig, raw string) error {
			count, err := strconv.Atoi(raw)
			if err != nil {
				return err
			}
			cfg.MaxRangeReclaims = common.Set(count)
			return nil
		},
		unset: func(cfg *admin.AlterGroupConfig) {
			cfg.MaxRangeReclaims = common.Unset[int]()
		},
	},
	{
		key:   "exception_initial_backoff",
		value: "duration, e.g. 10s",
		set: func(cfg *admin.AlterGroupConfig, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			cfg.ExceptionInitialBackoff = common.Set(duration)
			return nil
		},
		unset: func(cfg *admin.AlterGroupConfig) {
			cfg.ExceptionInitialBackoff = common.Unset[time.Duration]()
		},
	},
	{
		key:   "concurrency_override",
		value: "policy, allow or defer",
		set: func(cfg *admin.AlterGroupConfig, raw string) error {
			cfg.ConcurrencyOverride = common.Set(common.ConcurrencyPolicy(raw))
			return nil
		},
		unset: func(cfg *admin.AlterGroupConfig) {
			cfg.ConcurrencyOverride = common.Unset[common.ConcurrencyPolicy]()
		},
	},
}

func findGroupConfigKey(key string) (groupConfigKey, bool) {
	for _, entry := range groupConfigKeys {
		if entry.key == key {
			return entry, true
		}
	}
	return groupConfigKey{}, false
}

// messageFieldKey is one field of the message document: the dotted path
// config get prints and set accepts, its value shape, how a set patches the
// document, and how get reads it back.
type messageFieldKey struct {
	path  string
	value string
	patch func(options *common.MessageOptions, raw string) error
	read  func(options *common.MessageOptions) string
}

var messageFieldKeys = []messageFieldKey{
	{
		path:  "message.concurrency",
		value: "policy, allow or defer",
		patch: func(options *common.MessageOptions, raw string) error {
			options.Concurrency = common.ConcurrencyPolicy(raw)
			return nil
		},
		read: func(options *common.MessageOptions) string {
			return string(options.Concurrency)
		},
	},
	{
		path:  "message.timeout",
		value: "duration, e.g. 30s",
		patch: func(options *common.MessageOptions, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			options.Timeout = duration
			return nil
		},
		read: func(options *common.MessageOptions) string {
			return durationCell(options.Timeout)
		},
	},
	{
		path:  "message.retry.max_retries",
		value: "count, e.g. 3",
		patch: func(options *common.MessageOptions, raw string) error {
			count, err := strconv.Atoi(raw)
			if err != nil {
				return err
			}
			options.Retry.MaxRetries = count
			return nil
		},
		read: func(options *common.MessageOptions) string {
			return intCell(options.Retry.MaxRetries)
		},
	},
	{
		path:  "message.retry.base_delay",
		value: "duration, e.g. 1s",
		patch: func(options *common.MessageOptions, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			options.Retry.BaseDelay = duration
			return nil
		},
		read: func(options *common.MessageOptions) string {
			return durationCell(options.Retry.BaseDelay)
		},
	},
	{
		path:  "message.retry.max_delay",
		value: "duration, e.g. 5m",
		patch: func(options *common.MessageOptions, raw string) error {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			options.Retry.MaxDelay = duration
			return nil
		},
		read: func(options *common.MessageOptions) string {
			return durationCell(options.Retry.MaxDelay)
		},
	},
	{
		path:  "message.retry.exponent",
		value: "multiplier, e.g. 2",
		patch: func(options *common.MessageOptions, raw string) error {
			exponent, err := strconv.Atoi(raw)
			if err != nil {
				return err
			}
			options.Retry.Exponent = exponent
			return nil
		},
		read: func(options *common.MessageOptions) string {
			return intCell(options.Retry.Exponent)
		},
	},
}

func findMessageFieldKey(key string) (messageFieldKey, bool) {
	for _, field := range messageFieldKeys {
		if field.path == key {
			return field, true
		}
	}
	return messageFieldKey{}, false
}

// errUnknownGroupConfigKey rejects a key neither table knows, listing the
// vocabulary.
func errUnknownGroupConfigKey(key string) error {
	var known []string
	for _, entry := range groupConfigKeys {
		known = append(known, fmt.Sprintf("  %-26s %s", entry.key, entry.value))
	}
	for _, field := range messageFieldKeys {
		known = append(known, fmt.Sprintf("  %-26s %s", field.path, field.value))
	}
	return failUsage("unknown config key %q -- known keys:\n%s", key, strings.Join(known, "\n"))
}

// effectiveMessageOptions is the message document the group currently runs
// under: the override if one is set, else the default. A field set patches
// it so the one field changes and the rest keep their current values.
func effectiveMessageOptions(workers []*worker.Worker) (*common.MessageOptions, error) {
	for _, row := range workers {
		metadata, ok := row.Metadata.(map[string]any)
		if !ok {
			continue
		}
		layers, ok := metadata["message"].(map[string]any)
		if !ok {
			continue
		}
		value := layers["default"]
		if layers["override"] != nil {
			value = layers["override"]
		}
		return decodeMessageOptions(value)
	}
	return nil, errors.New(`no consumer kind of the group declares config key "message"`)
}

// decodeMessageOptions decodes one layer's message document.
func decodeMessageOptions(value any) (*common.MessageOptions, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var options common.MessageOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	// Retry may be absent from the document; field patches and reads assume
	// it isn't
	if options.Retry == nil {
		options.Retry = &retry.Policy{}
	}
	return &options, nil
}

// groupError maps a group config command failure to CLI output.
func groupError(topicName string, groupName string, err error) error {
	switch {
	case errors.Is(err, consumercontroller.ErrGroupNotFound):
		return failOp("consumer group %q not found on topic %q", groupName, topicName)
	case errors.Is(err, topic.ErrTopicNotFound):
		return errTopicNotFound(topicName)
	default:
		return translateAdminError(err)
	}
}
