package cli

import (
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/spf13/cobra"
)

func newMaintainRunCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the maintenance daemon until stopped",
		Long: "run discovers every maintenance duty in the deployment (partition\n" +
			"upkeep, retention, waterlines) and keeps them running. Safe to run\n" +
			"N-way: replicas coordinate through duty claims, so each duty's work\n" +
			"happens once per interval. Stop with SIGINT or SIGTERM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// once a stop begins, re-arm default delivery so a second signal
			// force-kills instead of being swallowed mid-drain
			go func() { <-ctx.Done(); stop() }()

			ds, closeDS, err := openDatastore(ctx, g.databaseURL)
			if err != nil {
				return err
			}
			defer closeDS()

			// unlike the one-shot commands, the daemon's log stream IS its
			// output -- full info level, still on stderr by convention
			fleet, err := maintain.NewFleetMaintainer(ds, &maintain.FleetMaintainerConfig{
				Logger: logger.NewDefaultLogger(os.Stderr, slog.LevelInfo),
			})
			if err != nil {
				return failOp("%s", err.Error())
			}

			if err := fleet.Register(ctx); err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					return errSystemNotRegistered()
				}
				return translateAdminError(err)
			}

			// a signal cancels ctx; Run drains its duty claims and returns nil
			if err := fleet.Run(ctx); err != nil {
				return failOp("%s", err.Error())
			}
			return nil
		},
	}
}
