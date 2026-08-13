package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/spf13/cobra"
)

func newAlertListCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the current alert per (alert, owner)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			heads, err := mAdmin.ListAlerts(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if quiet {
				printAlertKeys(out, heads)
			} else {
				printAlertsTable(out, heads)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "alert and owner only, one per line (for scripts)")
	return cmd
}

// ownerCell - "topic/orders", "system/system".
func ownerCell(owner *common.Owner) string {
	return fmt.Sprintf("%s/%s", owner.Kind(), owner.Name)
}

func printAlertKeys(w io.Writer, heads []*producer.MessageRow[alert.Alert]) {
	for _, head := range heads {
		fmt.Fprintf(w, "%s %s\n", head.Message.Name, ownerCell(head.Message.Owner))
	}
}

func printAlertsTable(w io.Writer, heads []*producer.MessageRow[alert.Alert]) {
	if len(heads) == 0 {
		fmt.Fprintln(w, "no alerts published")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tOWNER\tSTATUS\tSEVERITY\tSINCE\tMESSAGE")
	for _, head := range heads {
		published := head.Message
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			published.Name, ownerCell(published.Owner), published.Status,
			published.Severity, timeCell(head.CreatedAt), published.Message)
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(heads), "alert"))
}
