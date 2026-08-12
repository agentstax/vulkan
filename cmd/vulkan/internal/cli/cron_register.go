package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/spf13/cobra"
)

func newCronRegisterCmd(g *globalFlags) *cobra.Command {
	// Flags map 1:1 to croncontroller.CronJobConfig and are left unset by
	// default -- only the ones the operator actually passed reach the config,
	// so WithDefaults stays the single source of truth for everything else.
	var (
		scheduleExpr string
		timeout      time.Duration
		concurrency  string
		data         string
		metadata     string
	)

	cmd := &cobra.Command{
		Use:   "register <name> --schedule <expr>",
		Short: "Register a cron job (idempotent)",
		Long: "Register a cron job. Idempotent -- an existing name with the same\n" +
			"schedule/data/config is a no-op; a different one is rejected (that's\n" +
			"alter's job). Every job request is produced with the job's name as its routing\n" +
			"key -- consumers bind job names.",
		Example: "vulkan cron register reports.nightly --schedule \"30 4 * * *\" --data '{\"kind\":\"daily\"}'",
		Args:    requireCronJobName("register", "name: lowercase [a-z0-9._-], e.g. reports.nightly"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()
			f := cmd.Flags()

			if scheduleExpr == "" {
				return failUsage("register requires --schedule, e.g. --schedule \"30 4 * * *\"")
			}
			schedule, err := cron.ParseSchedule(scheduleExpr)
			if err != nil {
				return failUsage("invalid schedule: %s", err)
			}
			if data != "" && !json.Valid([]byte(data)) {
				return failUsage("--data must be valid JSON")
			}
			if metadata != "" && !json.Valid([]byte(metadata)) {
				return failUsage("--metadata must be valid JSON")
			}

			// Build a sparse config from only the flags that were passed.
			cfg := &croncontroller.CronJobConfig{}
			if f.Changed("timeout") {
				cfg.Timeout = timeout
			}
			if f.Changed("concurrency") {
				cfg.Concurrency = common.ConcurrencyPolicy(concurrency)
			}
			if f.Changed("metadata") {
				cfg.Metadata = json.RawMessage(metadata)
			}

			// Validate up front for a clean `invalid config:` message (a bad flag
			// value, exit 2) instead of the raw wrapped error the register returns.
			probe := *cfg
			probe.WithDefaults()
			if err := probe.Validate(); err != nil {
				return failUsage("invalid config: %s", err)
			}

			var dataJson any
			if f.Changed("data") {
				dataJson = json.RawMessage(data)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Existed-before decides "registered" vs "already registered".
			preJob, err := mAdmin.GetCronJob(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			// RegisterCronJob mutates cfg (WithDefaults) -- after this call cfg
			// holds the fully-defaulted config compared against the existing row.
			registered, err := mAdmin.RegisterCronJob(ctx, name, schedule, dataJson, cfg)
			if err != nil {
				if errors.Is(err, cron.ErrCronJobConfigMismatch) {
					return printCronMismatch(cmd.ErrOrStderr(), name, preJob, schedule, dataJson, cfg)
				}
				return translateAdminError(err)
			}

			if preJob != nil {
				fmt.Fprintf(out, "%s cron job %q already registered (id=%d) -- no changes\n", glyphOK(), name, registered.Id)
			} else {
				fmt.Fprintf(out, "%s registered cron job %q (id=%d), next scheduled time %s\n",
					glyphOK(), name, registered.Id, timeCell(registered.NextScheduledTime))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&scheduleExpr, "schedule", "", "cron spec or descriptor, UTC unless TZ= prefixed: \"30 4 * * 1\", \"@hourly\", \"TZ=America/New_York 0 9 * * *\" (required)")
	f.DurationVar(&timeout, "timeout", 0, "how long one job request's delivery may run, e.g. 30s (library default)")
	f.StringVar(&concurrency, "concurrency", "", "same-key policy when a job request lands while a previous one still runs: allow or defer (library default)")
	f.StringVar(&data, "data", "", "opaque JSON payload carried on every job request (default {})")
	f.StringVar(&metadata, "metadata", "", "opaque JSON metadata carried on every job request (default {})")

	// Duration flags default to 0, but 0 here means "unset -> library default",
	// not "0s". Blank the shown default so --help doesn't advertise 0s.
	f.Lookup("timeout").DefValue = ""

	return cmd
}

// printCronMismatch writes the diff between the existing job and what register
// tried to send (to stderr, w), then returns a printed error (exit 1). cfg is
// the fully defaulted config RegisterCronJob just compared and rejected.
func printCronMismatch(w io.Writer, name string, existing *cron.CronJob, schedule *cron.Schedule, data any, cfg *croncontroller.CronJobConfig) error {
	if existing == nil {
		// Lost a registration race between our GetCronJob and RegisterCronJob;
		// the row exists now but we didn't capture it. Report plainly rather
		// than invent a diff.
		return failOp("cron job %q already exists with a different configuration", name)
	}

	fmt.Fprintf(w, "error: cron job %q already exists with a different configuration\n\n", name)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  FIELD\tEXISTING\tREQUESTED")
	for _, d := range cronRegisterDiffs(existing, schedule, data, cfg) {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.name, d.existing, d.requested)
	}
	tw.Flush()

	fmt.Fprintf(w, "\nregister cannot change an existing cron job's config -- that's alter's job.\n")
	return failPrinted()
}

// cronRegisterDiffs returns one row per field where the existing job and the
// rejected register request differ, over exactly the fields the register
// compares.
func cronRegisterDiffs(existing *cron.CronJob, schedule *cron.Schedule, data any, cfg *croncontroller.CronJobConfig) []fieldDiff {
	var diffs []fieldDiff
	add := func(name, existingValue, requestedValue string) {
		if existingValue != requestedValue {
			diffs = append(diffs, fieldDiff{name, existingValue, requestedValue})
		}
	}
	add("Schedule", existing.Schedule, schedule.String())
	add("Concurrency", string(existing.Concurrency), string(cfg.Concurrency))
	add("Timeout", existing.Timeout.String(), cfg.Timeout.String())
	if jsonDiffers(existing.Data, requestedJson(data)) {
		diffs = append(diffs, fieldDiff{"Data", string(existing.Data), string(requestedJson(data))})
	}
	if jsonDiffers(existing.Metadata, requestedJson(cfg.Metadata)) {
		diffs = append(diffs, fieldDiff{"Metadata", string(existing.Metadata), string(requestedJson(cfg.Metadata))})
	}
	return diffs
}

// requestedJson mirrors what the register would store: nil -> {}.
func requestedJson(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage(`{}`)
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	marshaled, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return marshaled
}

// jsonDiffers matches jsonb's = -- the stored side comes back normalized, so
// key order and whitespace can't count.
func jsonDiffers(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return true
	}
	return !reflect.DeepEqual(av, bv)
}
