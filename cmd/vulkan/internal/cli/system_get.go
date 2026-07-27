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

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			sys, err := mAdmin.GetSystem(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			fmt.Fprintf(out, "%s system config\n", glyphOK())
			printSystemDetail(out, sys)
			return nil
		},
	}
}

func printSystemDetail(w io.Writer, s *system.System) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  CreatedAt\t%s\n", timeCell(s.CreatedAt))
	fmt.Fprintf(tw, "  UpdatedAt\t%s\n", timeCell(s.UpdatedAt))
	fmt.Fprintf(tw, "  AdvisorPollRate\t%s\n", s.AdvisorPollRate.String())
	fmt.Fprintf(tw, "  AdvisoryRepeatInterval\t%s\n", s.AdvisoryRepeatInterval.String())
	tw.Flush()
}
