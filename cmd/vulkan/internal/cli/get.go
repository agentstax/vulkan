package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

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

			// version hardcoded until the --schema-version flag lands.
			found, err := mAdmin.GetTopic(ctx, name, topic.SchemaVersion(1))
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
	fmt.Fprintf(tw, "  RetentionTTL\t%s\n", retentionDetail(t.RetentionTTL))
	fmt.Fprintf(tw, "  AllowDropPastCommitted\t%t\n", t.AllowDropPastCommitted)
	fmt.Fprintf(tw, "  IdempotencyKeyTTL\t%s\n", t.IdempotencyKeyTTL.String())
	fmt.Fprintf(tw, "  DisableDeliveryLog\t%t\n", t.DisableDeliveryLog)
	fmt.Fprintf(tw, "  JanitorPollRate\t%s\n", t.JanitorPollRate.String())
	fmt.Fprintf(tw, "  JanitorSweepBatchSize\t%d\n", t.JanitorSweepBatchSize)
	fmt.Fprintf(tw, "  WaterlinePollRate\t%s\n", t.WaterlinePollRate.String())
	tw.Flush()
}

// retentionDetail is the RetentionTTL cell: raw Go duration string, plus a day
// parenthetical when it's whole days ("720h0m0s (30d)"); "forever" for
// keep-indefinitely.
func retentionDetail(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	const day = 24 * time.Hour
	if d%day == 0 {
		return fmt.Sprintf("%s (%dd)", d, d/day)
	}
	return d.String()
}
