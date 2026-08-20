package cli

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentstax/vulkan/otelvulkan"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"github.com/spf13/cobra"
)

func newManagerRunCmd(g *globalFlags) *cobra.Command {
	var metricsAddress string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the system manager until stopped",
		Long: "run claims the system manager row and keeps every worker in the\n" +
			"deployment running -- partition upkeep, retention, committed advance, cron\n" +
			"scheduling -- with no consumer required. Safe to run N-way: replicas\n" +
			"coordinate through worker claims, so each worker's instance target\n" +
			"holds. With --metrics-address, also serves the deployment's\n" +
			"measurements as a Prometheus /metrics endpoint on that address.\n" +
			"Stop with SIGINT or SIGTERM.",
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
			runLogger := common.NewDefaultLogger(os.Stderr, slog.LevelInfo)

			systemManager, err := systemmanager.NewSystemManager(ds, &systemmanager.SystemManagerConfig{
				Logger: runLogger,
			})
			if err != nil {
				return failOp("%s", err.Error())
			}

			// a server failure cancels runCtx so the manager drains too
			runCtx, cancelRun := context.WithCancel(ctx)
			defer cancelRun()
			serverFailed := make(chan error, 1)
			if metricsAddress != "" {
				exporter, err := otelvulkan.NewExporter(ds, &otelvulkan.ExporterConfig{Logger: runLogger})
				if err != nil {
					return failOp("%s", err.Error())
				}
				if err := exporter.RegisterMetricInstruments(ctx); err != nil {
					if errors.Is(err, migrate.ErrNotRegistered) {
						return errSystemNotRegistered()
					}
					return failOp("%s", err.Error())
				}

				mux := http.NewServeMux()
				mux.Handle("/metrics", exporter.Handler())
				server := &http.Server{Addr: metricsAddress, Handler: mux}
				go func() {
					if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
						serverFailed <- err
						cancelRun()
					}
				}()
				defer func() {
					shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancelShutdown()
					_ = server.Shutdown(shutdownCtx)
					_ = exporter.Close(shutdownCtx)
				}()
				runLogger.InfoContext(ctx, "metrics endpoint serving", "address", metricsAddress)
			}

			// a signal cancels ctx; Run drains its worker claims and returns nil
			if err := systemManager.Run(runCtx); err != nil {
				if errors.Is(err, migrate.ErrNotRegistered) {
					return errSystemNotRegistered()
				}
				return failOp("%s", err.Error())
			}
			select {
			case err := <-serverFailed:
				return failOp("metrics endpoint: %s", err.Error())
			default:
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&metricsAddress, "metrics-address", "",
		"serve a Prometheus /metrics endpoint on this address (EX: :9464)")
	return cmd
}
