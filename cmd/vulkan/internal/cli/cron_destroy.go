package cli

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronDestroyCmd(g *globalFlags) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Permanently delete a cron job",
		Long: "Permanently delete a cron job. A job request already produced and not yet\n" +
			"consumed is not retracted -- destroy stops future requests only.",
		Args: requireCronJobName("destroy"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			// the confirmation prompt would pollute the json document stream
			if g.jsonOutput() && !yes {
				return failUsage("refusing to destroy %q without confirmation -- pass --yes with --output json", name)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			// Check order matters: a doomed call must never waste a prompt.
			found, err := mAdmin.GetCronJob(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}
			if found == nil {
				return errCronJobNotFound(name)
			}

			// confirm, unless --yes.
			if !yes {
				if !stdinIsTTY() {
					return failUsage("refusing to destroy %q without confirmation -- pass --yes in non-interactive contexts (e.g. CI)", name)
				}
				fmt.Fprintf(out, "This will PERMANENTLY delete cron job %q (id=%d, schedule %q).\n", name, found.Id, found.Schedule)
				fmt.Fprintln(out, "This cannot be undone.")
				fmt.Fprintln(out)
				fmt.Fprint(out, "Type the cron job name to confirm: ")

				typed, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(typed) != name {
					// No retry loop -- a piped wrong answer gets one shot, then out.
					fmt.Fprintln(out, "aborted: input did not match cron job name")
					return failPrinted()
				}
			}

			if !g.jsonOutput() {
				fmt.Fprintf(out, "destroying %q... ", name)
			}
			if err := mAdmin.DestroyCronJob(ctx, name); err != nil {
				if !g.jsonOutput() {
					fmt.Fprintln(out) // end the dangling "destroying..." line
				}
				if errors.Is(err, cron.ErrCronJobNotFound) {
					// dropped between our check and the delete
					return errCronJobNotFound(name)
				}
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, cronJobDestroyedDocument{CronJob: name, CronJobId: found.Id, Destroyed: true})
				return nil
			}
			fmt.Fprintln(out, "done")
			fmt.Fprintf(out, "%s cron job %q destroyed\n", glyphOK(), name)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation (for non-interactive/CI use)")
	return cmd
}

// cronJobDestroyedDocument is cron destroy's json result: a small
// what-happened record, never the dead row.
type cronJobDestroyedDocument struct {
	CronJob   string `json:"cron_job"`
	CronJobId int64  `json:"cron_job_id"`
	Destroyed bool   `json:"destroyed"`
}
