// Package context holds lifecycle-context glue shared by producers and consumers.
package context

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// LifecycleContext returns the application-lifetime context to pass to
// Register/Consume: cancelled on SIGINT/SIGTERM, which starts graceful
// wind-down (new work refused, queued work drains). Call stop on exit to
// release the signal handler.
//
//	ctx, stop := context.LifecycleContext()
//	defer stop()
func LifecycleContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
