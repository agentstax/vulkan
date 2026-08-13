package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/spf13/cobra"
)

func newAlertBindingsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bindings",
		Short: "List every consumer group binding",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			bindings, err := mAdmin.ListBindings(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			printBindingsTable(out, bindings)
			return nil
		},
	}
	return cmd
}

func printBindingsTable(w io.Writer, bindings []*consumercontroller.Binding) {
	if len(bindings) == 0 {
		fmt.Fprintln(w, "no bindings registered -- every group matches all events on its topic")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tTOPIC\tVERSION\tPATTERN")
	for _, binding := range bindings {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
			binding.GroupName, binding.TopicName, binding.SchemaVersion, binding.Pattern)
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(bindings), "binding"))
}
