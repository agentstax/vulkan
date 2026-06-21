package consumer

import (
	"context"
	"time"
)

// context cancel / deadline aware sleep
func SleepWithContext(ctx context.Context, duration time.Duration) error {
	t := time.NewTimer(duration)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
