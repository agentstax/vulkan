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

func newTopicRegisterCmd(g *globalFlags) *cobra.Command {
	// Flags map 1:1 to topic.Config and are left unset by default -- only the
	// ones the operator actually passed reach the Config, so WithDefaults stays
	// the single source of truth for everything else.
	var (
		partitionSize          int64
		retentionTTL           time.Duration
		allowDropPastCommitted bool
		idempotencyKeyTTL      time.Duration
		disableDeliveryLog     bool
		janitorPollRate        time.Duration
		janitorSweepBatchSize  int
	)

	cmd := &cobra.Command{
		Use:   "register <name>",
		Short: "Register a topic (idempotent)",
		Long: "Register a topic. Idempotent -- an existing name with the same config is a\n" +
			"no-op; a different config is rejected (that's alter's job). Topics are\n" +
			"addressed by id internally, so a name is safe to rename later.",
		Example: "# name: <domain>.<entity>[.<event>] -- e.g. orders.created\n" +
			"vulkan topic register orders.created --retention-ttl 720h",
		Args: requireTopicName("register", "name: <domain>.<entity>[.<event>], e.g. orders.created"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			// Build a sparse Config from only the flags that were passed.
			cfg := &topic.Config{}
			f := cmd.Flags()
			if f.Changed("partition-size") {
				cfg.PartitionSize = partitionSize
			}
			if f.Changed("retention-ttl") {
				cfg.RetentionTTL = retentionTTL
			}
			if f.Changed("allow-drop-past-committed") {
				cfg.AllowDropPastCommitted = allowDropPastCommitted
			}
			if f.Changed("idempotency-key-ttl") {
				cfg.IdempotencyKeyTTL = idempotencyKeyTTL
			}
			if f.Changed("disable-delivery-log") {
				cfg.DisableDeliveryLog = disableDeliveryLog
			}
			if f.Changed("janitor-poll-rate") {
				cfg.JanitorPollRate = janitorPollRate
			}
			if f.Changed("janitor-sweep-batch-size") {
				cfg.JanitorSweepBatchSize = janitorSweepBatchSize
			}

			// Validate up front for a clean `invalid config:` message (a bad flag
			// value, exit 2) instead of the raw wrapped error RegisterTopic returns.
			probe := *cfg
			probe.WithDefaults()
			if err := probe.Validate(); err != nil {
				return failUsage("invalid config: %s", err)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Existed-before decides "registered" vs "already registered".
			preTopic, err := mAdmin.GetTopic(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			// RegisterTopic mutates cfg (WithDefaults) -- after this call cfg holds
			// the fully-defaulted config that was compared against the existing row.
			registered, err := mAdmin.RegisterTopic(ctx, name, cfg)
			if err != nil {
				if errors.Is(err, topic.ErrTopicConfigMismatch) {
					return printMismatch(cmd.ErrOrStderr(), name, preTopic, cfg)
				}
				return translateAdminError(err)
			}

			out := cmd.OutOrStdout()
			if preTopic != nil {
				fmt.Fprintf(out, "%s topic %q already registered (id=%d) -- no changes\n",
					glyphOK(), name, registered.Id)
			} else {
				fmt.Fprintf(out, "%s registered topic %q (id=%d)\n", glyphOK(), name, registered.Id)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.Int64Var(&partitionSize, "partition-size", 0, "rows per partition (library default)")
	f.DurationVar(&retentionTTL, "retention-ttl", 0, "how long a message survives before retention drops it, e.g. 720h (library default)")
	f.BoolVar(&allowDropPastCommitted, "allow-drop-past-committed", false, "let retention drop data a lagging consumer hasn't committed (library default)")
	f.DurationVar(&idempotencyKeyTTL, "idempotency-key-ttl", 0, "how long a produce-retry claim survives, e.g. 1h (library default)")
	f.BoolVar(&disableDeliveryLog, "disable-delivery-log", false, "opt out of the per-attempt failure audit trail (library default)")
	f.DurationVar(&janitorPollRate, "janitor-poll-rate", 0, "how often the janitor loop ticks, e.g. 5s (library default)")
	f.IntVar(&janitorSweepBatchSize, "janitor-sweep-batch-size", 0, "rows deleted per sweep transaction (library default)")

	// Duration flags default to 0, but 0 here means "unset -> library default",
	// not "0s". Blank the shown default so --help doesn't advertise 0s as the
	// value; the int/bool flags already show nothing for their zero defaults.
	for _, name := range []string{"retention-ttl", "idempotency-key-ttl", "janitor-poll-rate"} {
		f.Lookup(name).DefValue = ""
	}

	return cmd
}

// printMismatch writes the diff between the existing topic and what register
// tried to send (to stderr, w), then returns a printed error (exit 1). want is
// the fully defaulted config RegisterTopic just compared and rejected.
func printMismatch(w io.Writer, name string, existing *topic.Topic, want *topic.Config) error {
	if existing == nil {
		// Lost a registration race between our GetTopic and RegisterTopic; the
		// row exists now but we didn't capture it. Report plainly rather than
		// invent a diff.
		return failOp("topic %q already exists with a different configuration", name)
	}

	fmt.Fprintf(w, "error: topic %q already exists with a different configuration\n\n", name)

	wantTopic := want.ToTopic(existing.Id, name, existing.CreatedAt, existing.UpdatedAt)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  FIELD\tEXISTING\tREQUESTED")
	for _, d := range topicFieldDiffs(existing, wantTopic) {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.name, d.existing, d.requested)
	}
	tw.Flush()

	fmt.Fprintf(w, "\nregister cannot change an existing topic's config -- that's alter's job.\n")
	return failPrinted()
}

type fieldDiff struct{ name, existing, requested string }

// topicFieldDiffs returns one row per field where the two topics differ, over
// exactly the fields UpsertTopic compares (Topic value equality).
func topicFieldDiffs(a, b *topic.Topic) []fieldDiff {
	var diffs []fieldDiff
	add := func(name, av, bv string) {
		if av != bv {
			diffs = append(diffs, fieldDiff{name, av, bv})
		}
	}
	add("PartitionSize", commaInt(a.PartitionSize), commaInt(b.PartitionSize))
	add("RetentionTTL", a.RetentionTTL.String(), b.RetentionTTL.String())
	add("AllowDropPastCommitted", fmt.Sprintf("%t", a.AllowDropPastCommitted), fmt.Sprintf("%t", b.AllowDropPastCommitted))
	add("IdempotencyKeyTTL", a.IdempotencyKeyTTL.String(), b.IdempotencyKeyTTL.String())
	add("DisableDeliveryLog", fmt.Sprintf("%t", a.DisableDeliveryLog), fmt.Sprintf("%t", b.DisableDeliveryLog))
	add("JanitorPollRate", a.JanitorPollRate.String(), b.JanitorPollRate.String())
	add("JanitorSweepBatchSize", fmt.Sprintf("%d", a.JanitorSweepBatchSize), fmt.Sprintf("%d", b.JanitorSweepBatchSize))
	return diffs
}
