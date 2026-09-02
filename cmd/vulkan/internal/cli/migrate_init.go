package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
)

func newMigrateInitCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the control-plane tables at its baseline (idempotent)",
		Long: "Stand up the shared control-plane tables every topic rides on, at version 1.\n" +
			"Idempotent -- safe to run on an already-initialized database. Run this once\n" +
			"before registering topics; `migrate system up` applies later versioned steps.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			client, closeClient, err := openClient(ctx, g.databaseURL, g.schema, slog.LevelError)
			if err != nil {
				return err
			}
			defer closeClient()

			if err := client.RegisterSystem(ctx, nil); err != nil {
				return translateAdminError(err)
			}

			if g.jsonOutput() {
				writeJSON(out, migrateInitDocument{Initialized: true, Current: 1})
				return nil
			}

			fmt.Fprintf(out, "%s system initialized (version 1)\n", glyphOK())
			return nil
		},
	}
}

// migrateInitDocument is migrate init's json result: the baseline exists.
type migrateInitDocument struct {
	Initialized bool  `json:"initialized"`
	Current     int64 `json:"current"`
}
