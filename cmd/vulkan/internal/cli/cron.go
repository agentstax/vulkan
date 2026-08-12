package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCronCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Register, inspect, alter, suspend, and destroy cron jobs",
	}

	cmd.AddCommand(newCronRegisterCmd(g))
	cmd.AddCommand(newCronListCmd(g))
	cmd.AddCommand(newCronGetCmd(g))
	cmd.AddCommand(newCronAlterCmd(g))
	cmd.AddCommand(newCronSuspendCmd(g))
	cmd.AddCommand(newCronUnsuspendCmd(g))
	cmd.AddCommand(newCronDestroyCmd(g))

	return cmd
}

// requireCronJobName is the shared Args rule for every single-job command:
// exactly one name, with a verb-specific usage line when it's missing.
func requireCronJobName(verb string, extraLines ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < 1 {
			msg := fmt.Sprintf("%s requires a cron job name\nusage: vulkan cron %s <name> [flags]", verb, verb)
			for _, line := range extraLines {
				msg += "\n" + line
			}
			return failUsage("%s", msg)
		}
		if len(args) > 1 {
			return failUsage("%s takes exactly one cron job name", verb)
		}
		return nil
	}
}

func errCronJobNotFound(name string) error {
	return failOp("cron job %q not found", name)
}
