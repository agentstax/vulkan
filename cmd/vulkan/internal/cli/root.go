package cli

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// Execute builds the command tree, runs it through fang, and returns the
// process exit code. version is the build-stamped string main passes in.
func Execute(ctx context.Context, version string) int {
	root, _ := newRootCmd()

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

// persisted global flags, read by subcommands off the root.
type globalFlags struct {
	databaseURL string
	json        bool
}

func newRootCmd() (*cobra.Command, *globalFlags) {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:   "vulkan",
		Short: "Admin CLI for Vulkan topics",
		Long: "vulkan is the privileged admin tool for a Vulkan deployment: register,\n" +
			"inspect, and destroy topics against the control-plane database.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.databaseURL, "database-url", "",
		"postgres:// connection URL (or set "+databaseURLEnv+")")
	pf.BoolVar(&g.json, "json", false, "emit machine-readable JSON instead of a table")

	root.AddCommand(newTopicCmd(g))

	return root, g
}
