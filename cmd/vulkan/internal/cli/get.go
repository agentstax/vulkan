package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicGetCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show every registered version of a topic and its drain/retire state",
		Args:  requireTopicName("get"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			health, err := mAdmin.FamilyHealth(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			// -q is the scriptable form: no output at all, the exit code IS the
			// answer (`if vulkan topic get -q X; then ...`).
			if quiet {
				if len(health) == 0 {
					return failPrinted()
				}
				return nil
			}

			if len(health) == 0 {
				fmt.Fprintf(out, "%s topic %q does not exist\n", glyphNo(), name)
				return failPrinted()
			}

			fmt.Fprintf(out, "%s topic %q -- %s\n", glyphOK(), name, pluralize(len(health), "registered version"))
			for i, h := range health {
				if i > 0 {
					fmt.Fprintln(out)
				}
				printVersionHealth(out, h)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "no output; exit code is the answer (0 exists, 1 not)")
	return cmd
}

// printTopicDetail shows the columns fixed at creation; the declared config
// lives under topic config get.
func printTopicDetail(w io.Writer, t *topic.Topic) {
	fmt.Fprintf(w, "\nv%d (id=%d)\n", t.SchemaVersion, t.Id)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  PartitionSize\t%s\n", commaInt(t.PartitionSize))
	tw.Flush()
}

// printVersionHealth is one registered version's full picture: its config
// (printTopicDetail), each bound group's drain progress against it, and the
// resulting retire verdict.
func printVersionHealth(w io.Writer, h *admin.VersionHealth) {
	printTopicDetail(w, h.Topic)

	ctw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(ctw, "  Compacted\t%t\n", h.Compacted)
	ctw.Flush()

	if len(h.Groups) > 0 {
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "  GROUP\tCOMMITTED\tHEAD\tLAG\tUNRESOLVED\tABANDONED\tOUTSTANDING\tAVG SELF-CLEAR")
		for _, group := range h.Groups {
			lag := group.GroupLag()
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
				group.ConsumerGroup, commaInt(lag.Committed), commaInt(lag.Head), commaInt(lag.Lag), lag.UnresolvedExceptions,
				group.AbandonedRoutines.Total, group.AbandonedRoutines.Outstanding, latencyCell(group.AbandonedRoutines.SelfClearLatencyAvg))
		}
		tw.Flush()
	}

	fmt.Fprintln(w)
	verdict := glyphNo()
	if h.Safe {
		verdict = glyphOK()
	}
	fmt.Fprintf(w, "  retire: %s %s\n", verdict, h.Reason)
}
