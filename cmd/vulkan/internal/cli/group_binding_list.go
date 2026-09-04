package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/spf13/cobra"
)

func newGroupBindingListCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every consumer group's declared binding set",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			bindings, err := client.System().Bindings(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				if bindings == nil {
					bindings = make([]*consume.Binding, 0)
				}
				writeJSON(out, bindings)
				return nil
			}

			printBindingsTable(out, bindings)
			return nil
		},
	}
	return cmd
}

func printBindingsTable(w io.Writer, bindings []*consume.Binding) {
	if len(bindings) == 0 {
		fmt.Fprintln(w, "no binding declarations -- every group receives every message on its topic")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tTOPIC\tSTATUS\tPATTERNS\tDECLARED BY\tDECLARED AT\tLAST ATTEMPT")
	for _, binding := range bindings {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			binding.GroupName,
			binding.TopicName,
			string(binding.Status),
			patternsCell(binding.Patterns),
			binding.DeclaredBy,
			timeCell(binding.DeclaredAt),
			timeCell(binding.AttemptedAt))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(bindings), "declaration"))
}

func patternsCell(patterns []string) string {
	if len(patterns) == 0 {
		return "(whole topic)"
	}
	return strings.Join(patterns, ",")
}
