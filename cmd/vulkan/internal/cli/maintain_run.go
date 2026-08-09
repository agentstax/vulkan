package cli

import (
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"github.com/spf13/cobra"
)

func newMaintainRunCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the maintenance daemon until stopped",
		Long: "run keeps every maintenance worker in the deployment running --\n" +
			"partition upkeep, retention, waterlines, cron scheduling -- with no\n" +
			"consumer required. Safe to run N-way: replicas coordinate through\n" +
			"worker claims, so each worker's instance target holds. Stop with\n" +
			"SIGINT or SIGTERM.",
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
			systemManager, err := systemmanager.NewSystemManager(ds, &systemmanager.SystemManagerConfig{
				Logger: logger.NewDefaultLogger(os.Stderr, slog.LevelInfo),
			})
			if err != nil {
				return failOp("%s", err.Error())
			}

			// a signal cancels ctx; Run drains its worker claims and returns nil
			if err := systemManager.Run(ctx); err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					return errSystemNotRegistered()
				}
				return failOp("%s", err.Error())
			}
			return nil
		},
	}
}
