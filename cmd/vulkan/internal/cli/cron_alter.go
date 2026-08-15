package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/spf13/cobra"
)

func newCronAlterCmd(g *globalFlags) *cobra.Command {
	// Flags map 1:1 to croncontroller.AlterCronJobConfig's sparse fields. Only
	// the ones the operator actually passed become set -- a patch, not a full
	// replace.
	// Name is absent: it is the routing key consumers bind, so a different
	// name is a different job, not a config change.
	var (
		scheduleExpr string
		timeout      time.Duration
		concurrency  string
		data         string
		metadata     string
	)

	cmd := &cobra.Command{
		Use:   "alter <name>",
		Short: "Change a registered cron job's config (only the fields you pass)",
		Long: "Change one or more config fields on an existing cron job. A patch --\n" +
			"fields you don't pass are left untouched.\n\n" +
			"A schedule change re-seeds the next scheduled time from the new schedule -- one\n" +
			"already due under the old schedule is dropped. Register calls still\n" +
			"passing the pre-alter config will fail with a config mismatch.",
		Example: "vulkan cron alter reports.nightly --schedule \"0 6 * * *\" --timeout 2m",
		Args:    requireCronJobName("alter"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()
			f := cmd.Flags()

			// Build a sparse patch from only the flags that were passed.
			cfg := &croncontroller.AlterCronJobConfig{}
			if f.Changed("schedule") {
				schedule, err := cron.ParseSchedule(scheduleExpr)
				if err != nil {
					return failUsage("invalid schedule: %s", err)
				}
				cfg.Schedule = schedule
			}
			if f.Changed("timeout") {
				cfg.Timeout = &timeout
			}
			if f.Changed("concurrency") {
				cfg.Concurrency = common.ConcurrencyPolicy(concurrency)
			}
			if f.Changed("data") {
				if !json.Valid([]byte(data)) {
					return failUsage("--data must be valid JSON")
				}
				cfg.Data = json.RawMessage(data)
			}
			if f.Changed("metadata") {
				if !json.Valid([]byte(metadata)) {
					return failUsage("--metadata must be valid JSON")
				}
				cfg.Metadata = json.RawMessage(metadata)
			}

			// Validate up front for a clean usage error (bad/absent flags, exit 2)
			// instead of the raw wrapped error AlterCronJob returns. Catches the
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
			before, err := mAdmin.GetCronJob(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			updated, err := mAdmin.AlterCronJob(ctx, name, cfg)
			if err != nil {
				if errors.Is(err, cron.ErrCronJobNotFound) {
					return errCronJobNotFound(name)
				}
				return translateAdminError(err)
			}

			printCronAlterResult(out, name, before, updated)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&scheduleExpr, "schedule", "", "cron spec or descriptor, UTC unless TZ= prefixed: \"30 4 * * 1\", \"@hourly\"")
	f.DurationVar(&timeout, "timeout", 0, "how long one job request's delivery may run, e.g. 30s")
	f.StringVar(&concurrency, "concurrency", "", "whether a job request runs while a previous one is still running: allow or defer")
	f.StringVar(&data, "data", "", "opaque JSON payload carried on every job request")
	f.StringVar(&metadata, "metadata", "", "opaque JSON metadata carried on every job request")

	return cmd
}

// printCronAlterResult writes the success line and an OLD -> NEW table over
// just the fields that actually changed. before may be nil only under a lost
// race (the job appeared between our GetCronJob and the alter) -- fall back to
// a bare line.
func printCronAlterResult(w io.Writer, name string, before, updated *cron.CronJob) {
	fmt.Fprintf(w, "%s altered cron job %q (id=%d)\n", glyphOK(), name, updated.Id)
	if before == nil {
		return
	}

	diffs := cronJobFieldDiffs(before, updated)
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

// cronJobFieldDiffs returns one row per field where the two jobs differ, over
// exactly the fields alter can change (plus the next scheduled time a schedule change
// re-seeds).
func cronJobFieldDiffs(a, b *cron.CronJob) []fieldDiff {
	var diffs []fieldDiff
	add := func(name, oldValue, newValue string) {
		if oldValue != newValue {
			diffs = append(diffs, fieldDiff{name, oldValue, newValue})
		}
	}
	add("Schedule", a.Schedule, b.Schedule)
	add("Concurrency", string(a.Concurrency), string(b.Concurrency))
	add("Timeout", a.Timeout.String(), b.Timeout.String())
	if jsonDiffers(a.Data, b.Data) {
		diffs = append(diffs, fieldDiff{"Data", string(a.Data), string(b.Data)})
	}
	if jsonDiffers(a.Metadata, b.Metadata) {
		diffs = append(diffs, fieldDiff{"Metadata", string(a.Metadata), string(b.Metadata)})
	}
	add("NextScheduledTime", timeCell(a.NextScheduledTime), timeCell(b.NextScheduledTime))
	return diffs
}

