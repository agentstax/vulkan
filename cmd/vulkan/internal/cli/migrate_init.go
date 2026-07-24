package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newMigrateInitCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the control-plane schema at its baseline (idempotent)",
		Long: "Stand up the shared control-plane schema every topic rides on, at version 1.\n" +
			"Idempotent -- safe to run on an already-initialized database. Run this once\n" +
			"before registering topics; `migrate system up` applies later versioned steps.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			mAdmin, _, closeAdmin, err := openAdmin(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeAdmin()

			if err := mAdmin.RegisterSystem(ctx); err != nil {
				return translateAdminError(err)
			}

			fmt.Fprintf(out, "%s system schema initialized (version 1)\n", glyphOK())
			return nil
		},
	}
}
