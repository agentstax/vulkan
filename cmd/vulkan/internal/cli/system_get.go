package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/system"
	"github.com/spf13/cobra"
)

func newSystemGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the singleton system config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			client, _, closeClient, err := openClient(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeClient()

			sys, err := client.System().Get(ctx)
			if err != nil {
				return translateAdminError(err)
			}
			if sys == nil {
				return failOp("system schema not registered -- run `vulkan migrate init` first")
			}

			if g.jsonOutput() {
				writeJSON(out, sys)
				return nil
			}

			fmt.Fprintf(out, "%s system config\n", glyphOK())
			printSystemDetail(out, sys)
			return nil
		},
	}
}

func printSystemDetail(w io.Writer, s *system.SystemData) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  CreatedAt\t%s\n", timeCell(s.CreatedAt))
	fmt.Fprintf(tw, "  UpdatedAt\t%s\n", timeCell(s.UpdatedAt))
	tw.Flush()
}
