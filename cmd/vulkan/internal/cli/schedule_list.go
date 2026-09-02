package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/spf13/cobra"
)

func newScheduleListCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every registered schedule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if quiet && g.jsonOutput() {
				return failUsage("--quiet and --output json cannot be combined")
			}

			client, _, closeClient, err := openClient(ctx, g.databaseURL, g.schema)
			if err != nil {
				return err
			}
			defer closeClient()

			schedules, err := client.ListSchedules(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, toScheduleDocuments(schedules))
				return nil
			}

			if quiet {
				printScheduleNames(out, schedules)
			} else {
				printSchedulesTable(out, schedules)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "names only, one per line (for scripts)")
	return cmd
}

func printScheduleNames(w io.Writer, schedules []*schedule.ScheduleData) {
	for _, row := range schedules {
		fmt.Fprintln(w, row.Name)
	}
}

func printSchedulesTable(w io.Writer, schedules []*schedule.ScheduleData) {
	if len(schedules) == 0 {
		fmt.Fprintln(w, "no schedules registered")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSCHEDULE\tCONCURRENCY\tTIMEOUT\tSUSPENDED\tNEXT\tLAST")
	for _, row := range schedules {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\t%s\n",
			row.Name, row.Expression, row.Concurrency, row.Timeout, row.Suspended,
			scheduleNextCell(row), scheduleLastCell(row))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(schedules), "schedule"))
}
