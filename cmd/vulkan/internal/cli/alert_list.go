package cli

import (
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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

			if quiet && g.jsonOutput() {
				return failUsage("--quiet and --output json cannot be combined")
			}

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			heads, err := client.ListAlerts(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				if heads == nil {
					heads = make([]*vulkan.MessageData[alert.Alert], 0)
				}
				writeJSON(out, heads)
				return nil
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

func printAlertKeys(w io.Writer, heads []*vulkan.MessageData[alert.Alert]) {
	for _, head := range heads {
		fmt.Fprintf(w, "%s %s\n", head.Message.Name, ownerCell(head.Message.Owner))
	}
}

func printAlertsTable(w io.Writer, heads []*vulkan.MessageData[alert.Alert]) {
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
