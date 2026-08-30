package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newScheduleCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Inspect, run, suspend, and destroy schedules",
	}

	cmd.AddCommand(newScheduleListCmd(g))
	cmd.AddCommand(newScheduleGetCmd(g))
	cmd.AddCommand(newScheduleSuspendCmd(g))
	cmd.AddCommand(newScheduleUnsuspendCmd(g))
	cmd.AddCommand(newScheduleRunCmd(g))
	cmd.AddCommand(newScheduleDestroyCmd(g))

	return cmd
}

// requireScheduleName is the shared Args rule for every single-schedule command:
// exactly one name, with a verb-specific usage line when it's missing.
func requireScheduleName(verb string, extraLines ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < 1 {
			msg := fmt.Sprintf("%s requires a schedule name\nusage: vulkan schedule %s <name> [flags]", verb, verb)
			for _, line := range extraLines {
				msg += "\n" + line
			}
			return failUsage("%s", msg)
		}
		if len(args) > 1 {
			return failUsage("%s takes exactly one schedule name", verb)
		}
		return nil
	}
}

func errScheduleNotFound(name string) error {
	return failOp("schedule %q not found", name)
}
