package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicGetCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show one topic's configuration, or report that it doesn't exist",
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

			found, err := mAdmin.GetTopic(ctx, name)
			if err != nil {
				return translateAdminError(err)
			}

			// -q is the scriptable form: no output at all, the exit code IS the
			// answer (`if vulkan topic get -q X; then ...`).
			if quiet {
				if found == nil {
					return failPrinted()
				}
				return nil
			}

			if found == nil {
				fmt.Fprintf(out, "%s topic %q does not exist\n", glyphNo(), name)
				return failPrinted()
			}
			printTopicDetail(out, found)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "no output; exit code is the answer (0 exists, 1 not)")
	return cmd
}

func printTopicDetail(w io.Writer, t *topic.Topic) {
	fmt.Fprintf(w, "%s topic %q exists (id=%d)\n\n", glyphOK(), t.Name, t.Id)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  CreatedAt\t%s\n", timeCell(t.CreatedAt))
	fmt.Fprintf(tw, "  UpdatedAt\t%s\n", timeCell(t.UpdatedAt))
	fmt.Fprintf(tw, "  PartitionSize\t%s\n", commaInt(t.PartitionSize))
	fmt.Fprintf(tw, "  RetentionTTL\t%s%s\n", retentionDetail(t.RetentionTTL), dayParenthetical(t.RetentionTTL))
	fmt.Fprintf(tw, "  AllowDropPastCommitted\t%t\n", t.AllowDropPastCommitted)
	fmt.Fprintf(tw, "  IdempotencyKeyTTL\t%s\n", t.IdempotencyKeyTTL.String())
	fmt.Fprintf(tw, "  DisableDeliveryLog\t%t\n", t.DisableDeliveryLog)
	fmt.Fprintf(tw, "  JanitorPollRate\t%s\n", t.JanitorPollRate.String())
	fmt.Fprintf(tw, "  JanitorSweepBatchSize\t%d\n", t.JanitorSweepBatchSize)
	tw.Flush()
}
