package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/spf13/cobra"
)

func newScheduleGetCmd(g *globalFlags) *cobra.Command {
	var (
		quiet    bool
		requests bool
		limit    int
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show a schedule's expression, config, and per-group run outcomes",
		Args:  requireScheduleName("get"),
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
			if quiet && g.jsonOutput() {
				return failUsage("--quiet and --output json cannot be combined")
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			row, err := mAdmin.GetSchedule(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				if row == nil {
					writeJSON(out, toScheduleGetDocument(name, nil, nil, nil))
					return failPrinted()
				}

				statuses, err := mAdmin.ScheduleStatus(ctx, name)
				if err != nil {
					return translateAdminError(err)
				}

				var listed []*schedule.JobRequestStatus
				if requests {
					listed, err = mAdmin.ScheduleRequests(ctx, name, limit)
					if err != nil {
						return translateAdminError(err)
					}
					if listed == nil {
						listed = make([]*schedule.JobRequestStatus, 0)
					}
				}

				writeJSON(out, toScheduleGetDocument(name, row, statuses, listed))
				return nil
			}

			// -q is the scriptable form: no output at all, the exit code IS the
			// answer (`if vulkan schedule get -q X; then ...`).
			if quiet {
				if row == nil {
					return failPrinted()
				}
				return nil
			}

			if row == nil {
				fmt.Fprintf(out, "%s schedule %q does not exist\n", glyphNo(), name)
				return failPrinted()
			}

			statuses, err := mAdmin.ScheduleStatus(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			fmt.Fprintf(out, "%s schedule %q (id=%d)\n", glyphOK(), name, row.Id)
			printScheduleDetail(out, row)
			printScheduleStatuses(out, statuses)

			if requests {
				listed, err := mAdmin.ScheduleRequests(ctx, name, limit)
				if err != nil {
					return translateAdminError(err)
				}
				printScheduleRequests(out, listed)
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

// scheduleDocument is one schedule's json shape -- the get-shape every
// schedule-echoing command shares. Durations render with units.
type scheduleDocument struct {
	ScheduleId      int64           `json:"schedule_id"`
	SystemId        int64           `json:"system_id"`
	TopicId         int64           `json:"topic_id"`
	GroupId         int64           `json:"group_id"`
	Schedule        string          `json:"schedule"`
	Expression      string          `json:"expression"`
	Concurrency     string          `json:"concurrency"`
	Timeout         string          `json:"timeout"`
	Suspended       bool            `json:"suspended"`
	Payload         json.RawMessage `json:"payload"`
	Metadata        json.RawMessage `json:"metadata"`
	NextScheduledAt time.Time       `json:"next_scheduled_at"`
	LastScheduledAt *time.Time      `json:"last_scheduled_at"` // null until the scheduler first produces the schedule
}

// scheduleGetDocument is schedule get's json result; the not-found case is data
// (exists false, schedule null), the exit code stays 1.
type scheduleGetDocument struct {
	Schedule string                       `json:"schedule"`
	Exists   bool                         `json:"exists"`
	Job      *scheduleDocument            `json:"row"`
	Groups   []*schedule.GroupStatus      `json:"groups"`
	Requests []*schedule.JobRequestStatus `json:"requests"` // null unless --requests
}

func toScheduleDocument(row *schedule.Schedule) scheduleDocument {
	return scheduleDocument{
		ScheduleId:      row.Id,
		SystemId:        row.SystemId,
		TopicId:         row.TopicId,
		GroupId:         row.ConsumerGroupId,
		Schedule:        row.Name,
		Expression:      row.Expression,
		Concurrency:     string(row.Concurrency),
		Timeout:         row.Timeout.String(),
		Suspended:       row.Suspended,
		Payload:         row.Payload,
		Metadata:        row.Metadata,
		NextScheduledAt: row.NextScheduledAt,
		LastScheduledAt: row.LastScheduledAt,
	}
}

func toScheduleDocuments(schedules []*schedule.Schedule) []scheduleDocument {
	documents := make([]scheduleDocument, 0, len(schedules))
	for _, row := range schedules {
		documents = append(documents, toScheduleDocument(row))
	}
	return documents
}

func toScheduleGetDocument(name string, row *schedule.Schedule, groups []*schedule.GroupStatus, requests []*schedule.JobRequestStatus) scheduleGetDocument {
	document := scheduleGetDocument{
		Schedule: name,
		Exists:   row != nil,
		Groups:   make([]*schedule.GroupStatus, 0, len(groups)),
		Requests: requests,
	}
	document.Groups = append(document.Groups, groups...)

	if row != nil {
		jobDocument := toScheduleDocument(row)
		document.Job = &jobDocument
	}
	return document
}

func printScheduleDetail(w io.Writer, row *schedule.Schedule) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  Schedule\t%s\n", row.Expression)
	fmt.Fprintf(tw, "  Concurrency\t%s\n", row.Concurrency)
	fmt.Fprintf(tw, "  Timeout\t%s\n", row.Timeout)
	fmt.Fprintf(tw, "  Suspended\t%t\n", row.Suspended)
	fmt.Fprintf(tw, "  Owner\t%s\n", scheduleOwnerCell(row))
	fmt.Fprintf(tw, "  Payload\t%s\n", row.Payload)
	fmt.Fprintf(tw, "  Metadata\t%s\n", row.Metadata)
	fmt.Fprintf(tw, "  NextScheduledAt\t%s\n", scheduleNextCell(row))
	fmt.Fprintf(tw, "  LastScheduledAt\t%s\n", scheduleLastCell(row))
	tw.Flush()
}

// printScheduleStatuses is one line per consumer group whose binding matches
// the schedule's name -- job request outcomes over the job_requests retention window.
func printScheduleStatuses(w io.Writer, statuses []*schedule.GroupStatus) {
	fmt.Fprintln(w)
	if len(statuses) == 0 {
		fmt.Fprintln(w, "  no consumer group is bound to this row's name")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  GROUP\tRAN\tSUCCEEDED\tFAILED\tSUPERSEDED")
	for _, status := range statuses {
		fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%d\n", status.ConsumerGroup, status.Ran, status.Succeeded, status.Failed, status.Superseded)
	}
	tw.Flush()
}

// printScheduleRequests is one line per (request, consumer group), newest
// request first -- requests older than the retention window are gone.
func printScheduleRequests(w io.Writer, statuses []*schedule.JobRequestStatus) {
	fmt.Fprintln(w)
	if len(statuses) == 0 {
		fmt.Fprintln(w, "  no job requests in the retention window")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  REQUEST\tSCHEDULED\tPRODUCED\tGROUP\tOUTCOME")
	for _, status := range statuses {
		// ScheduledAt is decoded from the payload in UTC -- render it in
		// the driver's zone like the columns beside it
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\n",
			status.MessageId, timeCell(status.ScheduledAt.Local()), timeCell(status.ProducedAt), status.ConsumerGroup, requestOutcomeCell(status))
	}
	tw.Flush()
}

// requestOutcomeCell names the replacing request inline, where the reader is
// already looking for it.
func requestOutcomeCell(status *schedule.JobRequestStatus) string {
	if status.Outcome == schedule.JobRequestSuperseded && status.SupersededBy != nil {
		return fmt.Sprintf("superseded by %d at %s", *status.SupersededBy, timeCell(*status.SupersededAt))
	}
	return string(status.Outcome)
}

// scheduleOwnerCell renders the exactly-one owner column the row carries.
func scheduleOwnerCell(row *schedule.Schedule) string {
	switch {
	case row.SystemId != 0:
		return fmt.Sprintf("system (id=%d)", row.SystemId)
	case row.TopicId != 0:
		return fmt.Sprintf("topic (id=%d)", row.TopicId)
	case row.ConsumerGroupId != 0:
		return fmt.Sprintf("consumer group (id=%d)", row.ConsumerGroupId)
	}
	return "none"
}

// scheduleNextCell - a suspended schedule's next_scheduled_at is stale by design
// (unsuspend re-seeds it), so show the state instead of a misleading time.
func scheduleNextCell(row *schedule.Schedule) string {
	if row.Suspended {
		return "suspended"
	}
	return timeCell(row.NextScheduledAt)
}

// scheduleLastCell - NULL until the scheduler first produces this schedule.
func scheduleLastCell(row *schedule.Schedule) string {
	if row.LastScheduledAt == nil {
		return "never"
	}
	return timeCell(*row.LastScheduledAt)
}
