package cli

import (
	"fmt"
	"io"

	"github.com/agentstax/vulkan/pkg/system"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	"github.com/spf13/cobra"
)

func newSystemAlterCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alter",
		Short: "Change the system config (only the fields you pass)",
		Long: "Change one or more fields on the singleton system config. A patch --\n" +
			"fields you don't pass are left untouched.\n\n" +
			"No alterable fields exist today; every call fails until a system-wide\n" +
			"knob lands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// Validate up front for a clean usage error (exit 2) instead of the raw
			// wrapped error AlterSystem returns. Catches the no-fields-set case too.
			cfg := &systemcontroller.AlterSystemConfig{}
			if err := cfg.Validate(); err != nil {
				return failUsage("%s", err)
			}

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			updated, err := mAdmin.AlterSystem(ctx, cfg)
			if err != nil {
				return translateAdminError(err)
			}

			printSystemAlterResult(out, updated)
			return nil
		},
	}

	return cmd
}

func printSystemAlterResult(w io.Writer, updated *system.System) {
	fmt.Fprintf(w, "%s altered system config\n", glyphOK())
	fmt.Fprintln(w, "  (no fields changed)")
}
