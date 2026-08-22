package cli

import (
	"context"
	"io"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// Execute builds the command tree, runs it through fang, and returns the
// process exit code.
func Execute(ctx context.Context, version string) int {
	root, g := newRootCmd()
	orderFlags(root)

	err := fang.Execute(
		ctx,
		root,
		fang.WithVersion(version),
		fang.WithErrorHandler(func(w io.Writer, _ fang.Styles, err error) {
			errorHandler(w, g, err)
		}),
	)
	if err != nil {
		return exitCode(err)
	}
	return 0
}

// orderFlags shows flags in definition order instead of fang's default
// alphabetical, giving every command the same shape: its own flags first (in
// the order they're declared), then the inherited globals, which cobra merges
// in last. The canonical trailing block is therefore always
//
//	... --database-url, --output, --help
//
// (--help last) -- keep it that way by declaring any new global on the ROOT's
// persistent flags before --help gets merged, and any new per-command flag
// before the merge. fang renders a single FLAGS block with no sub-grouping, so
// ordering is the only lever. (Root alone also shows fang's --version, which
// cobra appends after --help; that ordering isn't ours to control.)
func orderFlags(cmd *cobra.Command) {
	cmd.Flags().SortFlags = false
	for _, sub := range cmd.Commands() {
		orderFlags(sub)
	}
}

// persisted global flags, read by subcommands off the root.
type globalFlags struct {
	databaseURL string
	output      string
}

// jsonOutput reports whether --output json was passed. Commands branch on it
// once, after computing their result; the error handler branches on it too.
func (g *globalFlags) jsonOutput() bool {
	return g.output == "json"
}

func newRootCmd() (*cobra.Command, *globalFlags) {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:   "vulkan",
		Short: "Admin CLI for Vulkan deployments",
		Long: "vulkan is the privileged admin tool for a Vulkan deployment: manage\n" +
			"topics, schema migrations, and maintenance against the control-plane\n" +
			"database.",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if g.output != "text" && g.output != "json" {
				return failUsage("unrecognized output format: %q -- pass text or json", g.output)
			}
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.databaseURL, "database-url", "",
		"postgres:// connection URL (or set "+databaseURLEnv+")")
	pf.StringVar(&g.output, "output", "text",
		"output format: text or json (one document on stdout, errors as json on stderr)")

	root.AddCommand(newTopicCmd(g))
	root.AddCommand(newGroupCmd(g))
	root.AddCommand(newCronCmd(g))
	root.AddCommand(newAlertCmd(g))
	root.AddCommand(newMetricsCmd(g))
	root.AddCommand(newSystemCmd(g))
	root.AddCommand(newMigrateCmd(g))
	root.AddCommand(newManagerCmd(g))
	root.AddCommand(newExplainCmd(g))

	return root, g
}
