package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicGetCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show a topic, every payload version in its log, and each version's retire state",
		Args:  requireTopicName("get"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			if quiet && g.jsonOutput() {
				return failUsage("--quiet and --output json cannot be combined")
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			found, err := mAdmin.GetTopic(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() && found == nil {
				writeJSON(out, toTopicGetDocument(name, nil, nil))
				return failPrinted()
			}

			// -q is the scriptable form: no output at all, the exit code IS the
			// answer (`if vulkan topic get -q X; then ...`).
			if quiet {
				if found == nil {
					return failPrinted()
				}
				return nil
			}

			if found == nil {
				fmt.Fprintf(out, "%s topic %q does not exist\n", glyphNo(), name)
				return failPrinted()
			}

			health, err := mAdmin.TopicHealth(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, toTopicGetDocument(name, found, health))
				return nil
			}

			fmt.Fprintf(out, "%s topic %q -- %s\n", glyphOK(), name, pluralize(len(health), "payload version"))
			printTopicDetail(out, found)
			for _, h := range health {
				fmt.Fprintln(out)
				printVersionHealth(out, h)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "no output; exit code is the answer (0 exists, 1 not)")
	return cmd
}

// topicDocument is one topic row's json shape -- the get-shape every
// topic-echoing command shares. Durations render with units.
type topicDocument struct {
	TopicId                int64  `json:"topic_id"`
	SystemId               int64  `json:"system_id"`
	Topic                  string `json:"topic"`
	PartitionSize          int64  `json:"partition_size"`
	RetentionTTL           string `json:"retention_ttl"` // "0s" keeps messages forever
	AllowDropPastCommitted bool   `json:"allow_drop_past_committed"`
	IdempotencyKeyTTL      string `json:"idempotency_key_ttl"`
	DeliveryLogMode        string `json:"delivery_log_mode"`
}

// topicGetDocument is topic get's json result; the not-found case is data
// (exists false, topic null, versions empty), the exit code stays 1.
type topicGetDocument struct {
	Topic    string                  `json:"topic"`
	Exists   bool                    `json:"exists"`
	Config   *topicDocument          `json:"config"`
	Versions []versionHealthDocument `json:"versions"`
}

// versionHealthDocument is one payload version present in the log with its
// retire verdict.
type versionHealthDocument struct {
	Version         int64                     `json:"version"`
	Messages        int64                     `json:"messages"`
	CompactionHeads int64                     `json:"compaction_heads"`
	Groups          []groupVersionLagDocument `json:"groups"`
	Safe            bool                      `json:"safe"`
	Reason          string                    `json:"reason"`
}

type groupVersionLagDocument struct {
	Group                string `json:"group"`
	Unconsumed           int64  `json:"unconsumed"`
	UnresolvedExceptions int64  `json:"unresolved_exceptions"`
}

func toTopicDocument(found *topic.Topic) topicDocument {
	return topicDocument{
		TopicId:                found.Id,
		SystemId:               found.SystemId,
		Topic:                  found.Name,
		PartitionSize:          found.PartitionSize,
		RetentionTTL:           found.RetentionTTL.String(),
		AllowDropPastCommitted: found.AllowDropPastCommitted,
		IdempotencyKeyTTL:      found.IdempotencyKeyTTL.String(),
		DeliveryLogMode:        string(found.DeliveryLogMode),
	}
}

func toTopicDocuments(topics []*topic.Topic) []topicDocument {
	documents := make([]topicDocument, 0, len(topics))
	for _, found := range topics {
		documents = append(documents, toTopicDocument(found))
	}
	return documents
}

func toTopicGetDocument(name string, found *topic.Topic, health []*admin.VersionHealth) topicGetDocument {
	document := topicGetDocument{Topic: name, Exists: found != nil, Versions: make([]versionHealthDocument, 0, len(health))}
	if found != nil {
		config := toTopicDocument(found)
		document.Config = &config
	}
	for _, versionHealth := range health {
		document.Versions = append(document.Versions, toVersionHealthDocument(versionHealth))
	}
	return document
}

func toVersionHealthDocument(versionHealth *admin.VersionHealth) versionHealthDocument {
	groups := make([]groupVersionLagDocument, 0, len(versionHealth.Groups))
	for _, group := range versionHealth.Groups {
		groups = append(groups, groupVersionLagDocument{
			Group:                group.ConsumerGroup,
			Unconsumed:           group.Unconsumed,
			UnresolvedExceptions: group.UnresolvedExceptions,
		})
	}
	return versionHealthDocument{
		Version:         int64(versionHealth.Version),
		Messages:        versionHealth.Messages,
		CompactionHeads: versionHealth.CompactionHeads,
		Groups:          groups,
		Safe:            versionHealth.Safe,
		Reason:          versionHealth.Reason,
	}
}

// printTopicDetail shows the columns fixed at creation; the declared config
// lives under topic config get.
func printTopicDetail(w io.Writer, t *topic.Topic) {
	fmt.Fprintf(w, "\n(id=%d)\n", t.Id)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  PartitionSize\t%s\n", commaInt(t.PartitionSize))
	tw.Flush()
}

// printVersionHealth is one payload version's picture: how many rows sit at
// it, how many compaction heads point at it, each group's lag against it,
// and the resulting retire verdict.
func printVersionHealth(w io.Writer, h *admin.VersionHealth) {
	fmt.Fprintf(w, "  v%d\n", h.Version)

	ctw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(ctw, "    Messages\t%s\n", commaInt(h.Messages))
	fmt.Fprintf(ctw, "    CompactionHeads\t%s\n", commaInt(h.CompactionHeads))
	ctw.Flush()

	if len(h.Groups) > 0 {
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "    GROUP\tUNCONSUMED\tUNRESOLVED")
		for _, group := range h.Groups {
			fmt.Fprintf(tw, "    %s\t%s\t%d\n", group.ConsumerGroup, commaInt(group.Unconsumed), group.UnresolvedExceptions)
		}
		tw.Flush()
	}

	fmt.Fprintln(w)
	verdict := glyphNo()
	if h.Safe {
		verdict = glyphOK()
	}
	fmt.Fprintf(w, "    retire: %s %s\n", verdict, h.Reason)
}
