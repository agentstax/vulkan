package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/spf13/cobra"
)

func newTopicListCmd(g *globalFlags) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every registered topic",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			topics, err := mAdmin.ListTopics(ctx)
			if err != nil {
				return translateAdminError(err)
			}

			switch {
			case g.json:
				return printJSON(out, toTopicsJSON(topics))
			case quiet:
				printTopicNames(out, topics)
			default:
				printTopicsTable(out, topics)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&quiet, "quiet", "q", false, "names only, one per line (for scripts)")
	return cmd
}

func printTopicNames(w io.Writer, topics []*topic.Topic) {
	for _, t := range topics {
		fmt.Fprintln(w, t.Name)
	}
}

func printTopicsTable(w io.Writer, topics []*topic.Topic) {
	if len(topics) == 0 {
		fmt.Fprintln(w, "no topics registered")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCREATED\tUPDATED")
	for _, t := range topics {
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			t.Name,
			timeCell(t.CreatedAt),
			timeCell(t.UpdatedAt),
		)
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%s\n", pluralize(len(topics), "topic"))
}
