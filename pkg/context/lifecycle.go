// Package context holds lifecycle-context glue shared by producers and consumers.
package context

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentstax/vulkan/pkg/logger"
)

// LifecycleContext returns the application-lifetime context to pass to
// Register/Consume: cancelled on the first SIGINT/SIGTERM, which starts
// graceful wind-down (new work refused, queued work drains). A SECOND exit
// signal during the drain force-exits immediately (status 128+signum).
//
// log may be nil -- the warn-level default logger is used, which keeps the
// two graceful-shutdown info lines quiet and still surfaces a forced exit.
//
//	ctx, stop := context.LifecycleContext(nil)
//	defer stop()
func LifecycleContext(log logger.Logger) (context.Context, context.CancelFunc) {
	if log == nil {
		// specifically INFO here so lifecycle events are logged to user for clarity and understanding
		log = logger.NewDefaultLogger(os.Stdout, slog.LevelInfo)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-sigs:
			log.InfoContext(ctx, "graceful shutdown started (send signal again to force exit)", "signal", sig.String())
			cancel()

			// blocks until a second signal is recieved or <-ctx.Done() is handled
			sig = <-sigs
			log.WarnContext(ctx, "graceful shutdown interrupted by second signal -- forcing exit", "signal", sig.String())
			os.Exit(128 + int(sig.(syscall.Signal)))
		case <-ctx.Done():
			// stop() before any signal (clean exit) -- nothing to log.
		}
	}()

	stop := func() {
		signal.Stop(sigs)

		// must be set before cancel()
		// if draining = false -> normal exit no context was cancelled
		// if draining = true  -> ctx was already cancelled when we got here (the
		// signal handler beat us to it, or a prior stop() call did)
		draining := ctx.Err() != nil

		cancel()
		if draining {
			log.InfoContext(ctx, "graceful shutdown complete")
		}
	}
	return ctx, stop
}
