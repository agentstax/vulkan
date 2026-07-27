package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/system"
	"github.com/spf13/cobra"
)

func newSystemAlterCmd(g *globalFlags) *cobra.Command {
	// Flags map 1:1 to system.AlterConfig's pointer fields. Only the ones the
	// operator actually passed become non-nil -- a patch, not a full replace.
	var (
		advisorPollRate        time.Duration
		advisoryRepeatInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "alter",
		Short: "Change the system config (only the fields you pass)",
		Long: "Change one or more fields on the singleton system config. A patch --\n" +
			"fields you don't pass are left untouched.\n\n" +
			"The advisor duty snapshots this config at its Register, so an alter takes\n" +
			"effect on its next restart, not live.",
		Example: "vulkan system alter --advisor-poll-rate 2m --advisory-repeat-interval 4h",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// Build a sparse patch from only the flags that were passed.
			cfg := &system.AlterConfig{}
			f := cmd.Flags()
			if f.Changed("advisor-poll-rate") {
				cfg.AdvisorPollRate = &advisorPollRate
			}
			if f.Changed("advisory-repeat-interval") {
				cfg.AdvisoryRepeatInterval = &advisoryRepeatInterval
			}

			// Validate up front for a clean usage error (exit 2) instead of the raw
			// wrapped error AlterSystem returns. Catches the no-fields-set case too.
			if err := cfg.Validate(); err != nil {
				return failUsage("%s", err)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Snapshot before so we can show old -> new for what changed.
			before, err := mAdmin.GetSystem(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			updated, err := mAdmin.AlterSystem(ctx, cfg)
			if err != nil {
				return translateAdminError(err)
			}

			printSystemAlterResult(out, before, updated)
			return nil
		},
	}

	f := cmd.Flags()
	f.DurationVar(&advisorPollRate, "advisor-poll-rate", 0, "how often the advisor duty runs its structural checks, e.g. 2m")
	f.DurationVar(&advisoryRepeatInterval, "advisory-repeat-interval", 0, "how long a firing advisory stays quiet before re-emitting, e.g. 4h")

	return cmd
}

// printSystemAlterResult writes the success line and an OLD -> NEW table over
// just the fields that actually changed.
func printSystemAlterResult(w io.Writer, before, updated *system.System) {
	fmt.Fprintf(w, "%s altered system config\n", glyphOK())

	type diff struct{ name, old, new string }
	var diffs []diff
	if before.AdvisorPollRate != updated.AdvisorPollRate {
		diffs = append(diffs, diff{"AdvisorPollRate", before.AdvisorPollRate.String(), updated.AdvisorPollRate.String()})
	}
	if before.AdvisoryRepeatInterval != updated.AdvisoryRepeatInterval {
		diffs = append(diffs, diff{"AdvisoryRepeatInterval", before.AdvisoryRepeatInterval.String(), updated.AdvisoryRepeatInterval.String()})
	}

	if len(diffs) == 0 {
		fmt.Fprintln(w, "  (no fields changed)")
		return
	}

	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  FIELD\tOLD\tNEW")
	for _, d := range diffs {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.name, d.old, d.new)
	}
	tw.Flush()
}
