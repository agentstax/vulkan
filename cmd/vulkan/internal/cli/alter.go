package cli

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicAlterCmd(g *globalFlags) *cobra.Command {
	// Flags map 1:1 to topic.AlterConfig's pointer fields. Only the ones the
	// operator actually passed become non-nil -- a patch, not a full replace.
	// PartitionSize is absent (immutable -- baked into partition bounds), and
	// renaming is its own command.
	var (
		retentionTTL           time.Duration
		allowDropPastCommitted bool
		idempotencyKeyTTL      time.Duration
		disableDeliveryLog     bool
		janitorPollRate        time.Duration
		janitorSweepBatchSize  int
	)

	cmd := &cobra.Command{
		Use:   "alter <name>",
		Short: "Change a registered topic's config (only the fields you pass)",
		Long: "Change one or more config fields on an existing topic. A patch -- fields\n" +
			"you don't pass are left untouched. PartitionSize can't be altered (it's\n" +
			"baked into the partition layout); use `topic rename` to change the name.\n\n" +
			"Running producers/consumers snapshot config at their Register, so an alter\n" +
			"takes effect on their next restart, not live.",
		Example: "vulkan topic alter orders.created --retention-ttl 720h --janitor-sweep-batch-size 5000",
		Args:    requireTopicName("alter"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			// Build a sparse patch from only the flags that were passed. A bool
			// passed as --flag=false still counts as Changed -> the pointer is
			// set false, which is the whole point of the tri-state.
			cfg := &topic.AlterConfig{}
			f := cmd.Flags()
			if f.Changed("retention-ttl") {
				cfg.RetentionTTL = &retentionTTL
			}
			if f.Changed("allow-drop-past-committed") {
				cfg.AllowDropPastCommitted = &allowDropPastCommitted
			}
			if f.Changed("idempotency-key-ttl") {
				cfg.IdempotencyKeyTTL = &idempotencyKeyTTL
			}
			if f.Changed("disable-delivery-log") {
				cfg.DisableDeliveryLog = &disableDeliveryLog
			}
			if f.Changed("janitor-poll-rate") {
				cfg.JanitorPollRate = &janitorPollRate
			}
			if f.Changed("janitor-sweep-batch-size") {
				cfg.JanitorSweepBatchSize = &janitorSweepBatchSize
			}

			// Validate up front for a clean usage error (bad/absent flags, exit 2)
			// instead of the raw wrapped error AlterTopic returns. Catches the
			// no-fields-set case too.
			if err := cfg.Validate(); err != nil {
				return failUsage("%s", err)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Snapshot before so we can show old -> new for what changed.
			before, err := mAdmin.GetTopic(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			updated, err := mAdmin.AlterTopic(ctx, name, cfg)
			if err != nil {
				if errors.Is(err, topic.ErrTopicNotFound) {
					return errTopicNotFound(name)
				}
				return translateAdminError(err)
			}

			printAlterResult(out, name, before, updated)
			return nil
		},
	}

	f := cmd.Flags()
	f.DurationVar(&retentionTTL, "retention-ttl", 0, "how long a message survives before retention drops it, e.g. 720h")
	f.BoolVar(&allowDropPastCommitted, "allow-drop-past-committed", false, "let retention drop data a lagging consumer hasn't committed")
	f.DurationVar(&idempotencyKeyTTL, "idempotency-key-ttl", 0, "how long a produce-retry claim survives, e.g. 1h")
	f.BoolVar(&disableDeliveryLog, "disable-delivery-log", false, "stop writing the per-attempt failure audit trail")
	f.DurationVar(&janitorPollRate, "janitor-poll-rate", 0, "how often the janitor loop ticks, e.g. 5s")
	f.IntVar(&janitorSweepBatchSize, "janitor-sweep-batch-size", 0, "rows deleted per sweep transaction")

	return cmd
}

// printAlterResult writes the success line and an OLD -> NEW table over just the
// fields that actually changed. before may be nil only under a lost race (the
// topic appeared between our GetTopic and the alter) -- fall back to a bare line.
func printAlterResult(w io.Writer, name string, before, updated *topic.Topic) {
	fmt.Fprintf(w, "%s altered topic %q (id=%d)\n", glyphOK(), name, updated.Id)
	if before == nil {
		return
	}

	diffs := topicFieldDiffs(before, updated)
	if len(diffs) == 0 {
		fmt.Fprintln(w, "  (no fields changed)")
		return
	}

	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  FIELD\tOLD\tNEW")
	for _, d := range diffs {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.name, d.existing, d.requested)
	}
	tw.Flush()
}
