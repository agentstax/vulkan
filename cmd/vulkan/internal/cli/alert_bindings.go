package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/spf13/cobra"
)

func newAlertBindingsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bindings",
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

			declarations, err := client.ListBindingDeclarations(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				if declarations == nil {
					declarations = make([]*consumergroup.BindingDeclaration, 0)
				}
				writeJSON(out, declarations)
				return nil
			}

			printBindingDeclarationsTable(out, declarations)
			return nil
		},
	}
	return cmd
}

func printBindingDeclarationsTable(w io.Writer, declarations []*consumergroup.BindingDeclaration) {
	if len(declarations) == 0 {
		fmt.Fprintln(w, "no binding declarations -- every group matches all events on its topic")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tTOPIC\tSTATUS\tPATTERNS\tDECLARED BY\tDECLARED AT\tLAST ATTEMPT")
	for _, declaration := range declarations {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			declaration.GroupName,
			declaration.TopicName,
			string(declaration.Status),
			patternsCell(declaration.Patterns),
			declaration.DeclaredBy,
			timeCell(declaration.DeclaredAt),
			timeCell(declaration.AttemptedAt))
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(declarations), "declaration"))
}

func patternsCell(patterns []string) string {
	if len(patterns) == 0 {
		return "(whole topic)"
	}
	return strings.Join(patterns, ",")
}
