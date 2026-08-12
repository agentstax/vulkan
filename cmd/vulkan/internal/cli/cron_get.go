package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronGetCmd(g *globalFlags) *cobra.Command {
	var (
		quiet    bool
		requests bool
		limit    int
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show a cron job's schedule, config, and per-group run outcomes",
		Args:  requireCronJobName("get"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()
			f := cmd.Flags()

			if f.Changed("limit") && !requests {
				return failUsage("--limit only applies to the --requests listing")
			}
			if limit <= 0 {
				return failUsage("--limit must be > 0, got %d", limit)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			job, err := mAdmin.GetCronJob(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			// -q is the scriptable form: no output at all, the exit code IS the
			// answer (`if vulkan cron get -q X; then ...`).
			if quiet {
				if job == nil {
					return failPrinted()
				}
				return nil
			}

			if job == nil {
				fmt.Fprintf(out, "%s cron job %q does not exist\n", glyphNo(), name)
				return failPrinted()
			}

			statuses, err := mAdmin.CronJobStatus(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			fmt.Fprintf(out, "%s cron job %q (id=%d)\n", glyphOK(), name, job.Id)
			printCronJobDetail(out, job)
			printCronJobStatuses(out, statuses)

			if requests {
				listed, err := mAdmin.CronJobRequests(ctx, name, limit)
				if err != nil {
					return translateAdminError(err)
				}
				printCronJobRequests(out, listed)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "no output; exit code is the answer (0 exists, 1 not)")
	f.BoolVar(&requests, "requests", false, "also list the newest job requests, one line per (request, consumer group)")
	f.IntVar(&limit, "limit", 20, "how many of the newest requests --requests lists")
	return cmd
}

func printCronJobDetail(w io.Writer, job *cron.CronJob) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  Schedule\t%s\n", job.Schedule)
	fmt.Fprintf(tw, "  Concurrency\t%s\n", job.Concurrency)
	fmt.Fprintf(tw, "  Timeout\t%s\n", job.Timeout)
	fmt.Fprintf(tw, "  Suspended\t%t\n", job.Suspended)
	fmt.Fprintf(tw, "  Owner\t%s\n", cronOwnerCell(job))
	fmt.Fprintf(tw, "  Data\t%s\n", job.Data)
	fmt.Fprintf(tw, "  Metadata\t%s\n", job.Metadata)
	fmt.Fprintf(tw, "  NextScheduledTime\t%s\n", cronNextCell(job))
	fmt.Fprintf(tw, "  LastScheduledTime\t%s\n", cronLastCell(job))
	tw.Flush()
}

// printCronJobStatuses is one line per consumer group whose binding matches
// the job's name -- job request outcomes over the job_requests retention window.
func printCronJobStatuses(w io.Writer, statuses []*cron.GroupStatus) {
	fmt.Fprintln(w)
	if len(statuses) == 0 {
		fmt.Fprintln(w, "  no consumer group is bound to this job's name")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  GROUP\tRAN\tSUCCEEDED\tFAILED\tSUPERSEDED")
	for _, status := range statuses {
		fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%d\n", status.ConsumerGroup, status.Ran, status.Succeeded, status.Failed, status.Superseded)
	}
	tw.Flush()
}

// printCronJobRequests is one line per (request, consumer group), newest
// request first -- requests older than the retention window are gone.
func printCronJobRequests(w io.Writer, statuses []*cron.JobRequestStatus) {
	fmt.Fprintln(w)
	if len(statuses) == 0 {
		fmt.Fprintln(w, "  no job requests in the retention window")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  REQUEST\tSCHEDULED\tPRODUCED\tGROUP\tOUTCOME")
	for _, status := range statuses {
		// ScheduledTime is decoded from the payload in UTC -- render it in
		// the driver's zone like the columns beside it
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\n",
			status.MessageId, timeCell(status.ScheduledTime.Local()), timeCell(status.ProducedAt), status.ConsumerGroup, requestOutcomeCell(status))
	}
	tw.Flush()
}

// requestOutcomeCell names the replacing request inline, where the reader is
// already looking for it.
func requestOutcomeCell(status *cron.JobRequestStatus) string {
	if status.Outcome == cron.JobRequestSuperseded && status.SupersededBy != nil {
		return fmt.Sprintf("superseded by %d at %s", *status.SupersededBy, timeCell(*status.SupersededAt))
	}
	return string(status.Outcome)
}

// cronOwnerCell renders the exactly-one owner column the row carries.
func cronOwnerCell(job *cron.CronJob) string {
	switch {
	case job.SystemId != 0:
		return fmt.Sprintf("system (id=%d)", job.SystemId)
	case job.TopicId != 0:
		return fmt.Sprintf("topic (id=%d)", job.TopicId)
	case job.ConsumerGroupId != 0:
		return fmt.Sprintf("consumer group (id=%d)", job.ConsumerGroupId)
	}
	return "none"
}

// cronNextCell - a suspended job's next_scheduled_time is stale by design
// (unsuspend re-seeds it), so show the state instead of a misleading time.
func cronNextCell(job *cron.CronJob) string {
	if job.Suspended {
		return "suspended"
	}
	return timeCell(job.NextScheduledTime)
}

// cronLastCell - NULL until the scheduler first produces this job.
func cronLastCell(job *cron.CronJob) string {
	if job.LastScheduledTime == nil {
		return "never"
	}
	return timeCell(*job.LastScheduledTime)
}
