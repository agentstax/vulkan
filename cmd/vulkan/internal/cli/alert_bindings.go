package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/consumer/binding"
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

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			declarations, err := mAdmin.ListDeclarations(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			printDeclarationsTable(out, declarations)
			return nil
		},
	}
	return cmd
}

func printDeclarationsTable(w io.Writer, declarations []*binding.Declaration) {
	if len(declarations) == 0 {
		fmt.Fprintln(w, "no binding declarations -- every group matches all events on its topic")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tTOPIC\tVERSION\tSTATUS\tPATTERNS\tDECLARED BY\tDECLARED AT\tLAST ATTEMPT")
	for _, declaration := range declarations {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			declaration.GroupName,
			declaration.TopicName,
			declaration.SchemaVersion,
			string(declaration.Status),
			patternsCell(declaration.Patterns),
			declaration.DeclaredBy,
			timeCell(declaration.DeclaredAt),
			timeCell(declaration.AttemptAt))
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
