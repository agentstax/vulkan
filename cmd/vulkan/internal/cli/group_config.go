package cli

import (
	"encoding/json"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newGroupConfigCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read a consumer group's config",
		Long: `A group's config comes from the cfg your code passes to consumer Register,
and is applied every time that runs. Changing a value means changing that
code and redeploying; this command only reads.

Each consumer kind declares its own keys, so the WORKER column names the row
a value came from and there is no single default to show.

A running consumer reads its config when it claims work, so a change takes
effect at its next claim, not live.`,
	}

	cmd.AddCommand(newGroupConfigGetCmd(g))

	return cmd
}

// messageFieldKey is one field of the message document: the dotted path
// config get prints, and how get reads it back.
type messageFieldKey struct {
	path string
	read func(options *common.MessageOptions) string
}

var messageFieldKeys = []messageFieldKey{
	{
		path: "message.concurrency",
		read: func(options *common.MessageOptions) string {
			return string(options.Concurrency)
		},
	},
	{
		path: "message.timeout",
		read: func(options *common.MessageOptions) string {
			return durationCell(options.Timeout)
		},
	},
	{
		path: "message.retry.max_retries",
		read: func(options *common.MessageOptions) string {
			return intCell(options.Retry.MaxRetries)
		},
	},
	{
		path: "message.retry.base_delay",
		read: func(options *common.MessageOptions) string {
			return durationCell(options.Retry.BaseDelay)
		},
	},
	{
		path: "message.retry.max_delay",
		read: func(options *common.MessageOptions) string {
			return durationCell(options.Retry.MaxDelay)
		},
	},
	{
		path: "message.retry.exponent",
		read: func(options *common.MessageOptions) string {
			return intCell(options.Retry.Exponent)
		},
	},
}

// decodeMessageOptions decodes a worker row's stored message document.
func decodeMessageOptions(value any) (*common.MessageOptions, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var options common.MessageOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	// Retry may be absent from the document; field reads assume it isn't
	if options.Retry == nil {
		options.Retry = &common.RetryPolicy{}
	}
	return &options, nil
}

// groupError maps a group config command failure to CLI output.
func groupError(topicName string, groupName string, err error) error {
	switch {
	case errors.Is(err, consumergroup.ErrGroupNotFound):
		return failOp("consumer group %q not found on topic %q", groupName, topicName)
	case errors.Is(err, topic.ErrTopicNotFound):
		return errTopicNotFound(topicName)
	default:
		return translateAdminError(err)
	}
}
