package cli

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// Execute builds the command tree, runs it through fang, and returns the
// process exit code.
func Execute(ctx context.Context, version string) int {
	root, _ := newRootCmd()
	orderFlags(root)

	err := fang.Execute(
		ctx,
		root,
		fang.WithVersion(version),
		fang.WithErrorHandler(errorHandler),
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
//	... --database-url, --help
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
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.databaseURL, "database-url", "",
		"postgres:// connection URL (or set "+databaseURLEnv+")")

	root.AddCommand(newTopicCmd(g))
	root.AddCommand(newGroupCmd(g))
	root.AddCommand(newCronCmd(g))
	root.AddCommand(newAlertCmd(g))
	root.AddCommand(newMetricsCmd(g))
	root.AddCommand(newSystemCmd(g))
	root.AddCommand(newMigrateCmd(g))
	root.AddCommand(newManagerCmd(g))

	return root, g
}
