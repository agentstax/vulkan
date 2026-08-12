package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronListCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every registered cron job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			jobs, err := mAdmin.ListCronJobs(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if quiet {
				printCronJobNames(out, jobs)
			} else {
				printCronJobsTable(out, jobs)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "names only, one per line (for scripts)")
	return cmd
}

func printCronJobNames(w io.Writer, jobs []*cron.CronJob) {
	for _, job := range jobs {
		fmt.Fprintln(w, job.Name)
	}
}

func printCronJobsTable(w io.Writer, jobs []*cron.CronJob) {
	if len(jobs) == 0 {
		fmt.Fprintln(w, "no cron jobs registered")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSCHEDULE\tCONCURRENCY\tTIMEOUT\tSUSPENDED\tNEXT\tLAST")
	for _, job := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\t%s\n",
			job.Name, job.Schedule, job.Concurrency, job.Timeout, job.Suspended,
			cronNextCell(job), cronLastCell(job))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(jobs), "cron job"))
}
