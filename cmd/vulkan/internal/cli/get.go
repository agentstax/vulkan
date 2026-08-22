package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicGetCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show every registered version of a topic and its drain/retire state",
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

			health, err := mAdmin.FamilyHealth(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, toTopicGetDocument(name, health))
				if len(health) == 0 {
					return failPrinted()
				}
				return nil
			}

			// -q is the scriptable form: no output at all, the exit code IS the
			// answer (`if vulkan topic get -q X; then ...`).
			if quiet {
				if len(health) == 0 {
					return failPrinted()
				}
				return nil
			}

			if len(health) == 0 {
				fmt.Fprintf(out, "%s topic %q does not exist\n", glyphNo(), name)
				return failPrinted()
			}

			fmt.Fprintf(out, "%s topic %q -- %s\n", glyphOK(), name, pluralize(len(health), "registered version"))
			for i, h := range health {
				if i > 0 {
					fmt.Fprintln(out)
				}
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
	Version                int64  `json:"version"`
	PartitionSize          int64  `json:"partition_size"`
	RetentionTTL           string `json:"retention_ttl"` // "0s" keeps messages forever
	AllowDropPastCommitted bool   `json:"allow_drop_past_committed"`
	IdempotencyKeyTTL      string `json:"idempotency_key_ttl"`
	DeliveryLogMode        string `json:"delivery_log_mode"`
}

// topicGetDocument is topic get's json result; the not-found case is data
// (exists false, versions empty), the exit code stays 1.
type topicGetDocument struct {
	Topic    string                  `json:"topic"`
	Exists   bool                    `json:"exists"`
	Versions []versionHealthDocument `json:"versions"`
}

type versionHealthDocument struct {
	Topic     topicDocument                   `json:"topic"`
	Compacted bool                            `json:"compacted"`
	Groups    []consumerGroupSnapshotDocument `json:"groups"`
	Safe      bool                            `json:"safe"`
	Reason    string                          `json:"reason"`
}

type consumerGroupSnapshotDocument struct {
	Group             string                           `json:"group"`
	Cursor            metrics.CursorSnapshot           `json:"cursor"`
	Exceptions        exceptionSnapshotDocument        `json:"exceptions"`
	OpenLeases        int64                            `json:"open_leases"`
	AbandonedRoutines abandonedRoutineSnapshotDocument `json:"abandoned_routines"`
}

type exceptionSnapshotDocument struct {
	Ready               int64  `json:"ready"`
	Inflight            int64  `json:"inflight"`
	Deferred            int64  `json:"deferred"`
	Dead                int64  `json:"dead"`
	OldestUnresolvedAge string `json:"oldest_unresolved_age"`
}

type abandonedRoutineSnapshotDocument struct {
	Outstanding         int64  `json:"outstanding"`
	Total               int64  `json:"total"`
	SelfClearLatencyAvg string `json:"self_clear_latency_avg"`
}

func toTopicDocument(found *topic.Topic) topicDocument {
	return topicDocument{
		TopicId:                found.Id,
		SystemId:               found.SystemId,
		Topic:                  found.Name,
		Version:                int64(found.SchemaVersion),
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

func toTopicGetDocument(name string, family []*admin.VersionHealth) topicGetDocument {
	versions := make([]versionHealthDocument, 0, len(family))
	for _, versionHealth := range family {
		versions = append(versions, toVersionHealthDocument(versionHealth))
	}
	return topicGetDocument{Topic: name, Exists: len(family) > 0, Versions: versions}
}

func toVersionHealthDocument(versionHealth *admin.VersionHealth) versionHealthDocument {
	groups := make([]consumerGroupSnapshotDocument, 0, len(versionHealth.Groups))
	for _, group := range versionHealth.Groups {
		groups = append(groups, toConsumerGroupSnapshotDocument(&group))
	}
	return versionHealthDocument{
		Topic:     toTopicDocument(versionHealth.Topic),
		Compacted: versionHealth.Compacted,
		Groups:    groups,
		Safe:      versionHealth.Safe,
		Reason:    versionHealth.Reason,
	}
}

func toConsumerGroupSnapshotDocument(snapshot *metrics.ConsumerGroupSnapshot) consumerGroupSnapshotDocument {
	return consumerGroupSnapshotDocument{
		Group:             snapshot.ConsumerGroup,
		Cursor:            snapshot.Cursor,
		Exceptions:        toExceptionSnapshotDocument(&snapshot.Exceptions),
		OpenLeases:        snapshot.OpenLeases,
		AbandonedRoutines: toAbandonedRoutineSnapshotDocument(&snapshot.AbandonedRoutines),
	}
}

func toExceptionSnapshotDocument(snapshot *metrics.ExceptionSnapshot) exceptionSnapshotDocument {
	return exceptionSnapshotDocument{
		Ready:               snapshot.Ready,
		Inflight:            snapshot.Inflight,
		Deferred:            snapshot.Deferred,
		Dead:                snapshot.Dead,
		OldestUnresolvedAge: snapshot.OldestUnresolvedAge.String(),
	}
}

func toAbandonedRoutineSnapshotDocument(snapshot *metrics.AbandonedRoutineSnapshot) abandonedRoutineSnapshotDocument {
	return abandonedRoutineSnapshotDocument{
		Outstanding:         snapshot.Outstanding,
		Total:               snapshot.Total,
		SelfClearLatencyAvg: snapshot.SelfClearLatencyAvg.String(),
	}
}

// printTopicDetail shows the columns fixed at creation; the declared config
// lives under topic config get.
func printTopicDetail(w io.Writer, t *topic.Topic) {
	fmt.Fprintf(w, "\nv%d (id=%d)\n", t.SchemaVersion, t.Id)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  PartitionSize\t%s\n", commaInt(t.PartitionSize))
	tw.Flush()
}

// printVersionHealth is one registered version's full picture: its config
// (printTopicDetail), each bound group's drain progress against it, and the
// resulting retire verdict.
func printVersionHealth(w io.Writer, h *admin.VersionHealth) {
	printTopicDetail(w, h.Topic)

	ctw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(ctw, "  Compacted\t%t\n", h.Compacted)
	ctw.Flush()

	if len(h.Groups) > 0 {
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "  GROUP\tCOMMITTED\tHEAD\tLAG\tUNRESOLVED\tABANDONED\tOUTSTANDING\tAVG SELF-CLEAR")
		for _, group := range h.Groups {
			lag := group.GroupLag()
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
				group.ConsumerGroup, commaInt(lag.Committed), commaInt(lag.Head), commaInt(lag.Lag), lag.UnresolvedExceptions,
				group.AbandonedRoutines.Total, group.AbandonedRoutines.Outstanding, latencyCell(group.AbandonedRoutines.SelfClearLatencyAvg))
		}
		tw.Flush()
	}

	fmt.Fprintln(w)
	verdict := glyphNo()
	if h.Safe {
		verdict = glyphOK()
	}
	fmt.Fprintf(w, "  retire: %s %s\n", verdict, h.Reason)
}
