// Scenario 11 -- a handler that runs longer than its lease.
//
// Video transcoding: most jobs finish in a minute, some take an hour. The
// handler cannot know up front.
//
// Concepts held before domain code (13): the 8 from scenario 03, plus
// MessageOptions.Timeout, MessageMax, the producer-side per-message
// Timeout request, the lease = Timeout + grace + margins formula, and the
// ctx.Done() contract inside the handler.
//
// Traps hit:
//   - There is no way to extend a lease from inside the handler (SQS
//     ChangeMessageVisibility, JetStream InProgress). The only knob is a
//     ceiling chosen before the work starts: set it to the worst case and
//     a crashed worker's message waits an hour to be reclaimed; set it to
//     the common case and the long jobs time out and retry forever.
//   - The timeout is decided by three parties -- the message's request,
//     the consumer's default, the consumer's MessageMax clamp -- and a
//     message asking for more than MessageMax is clamped silently (a Warn
//     log, not an error).
//   - A handler that ignores ctx.Done() past the timeout is abandoned, not
//     killed: it keeps running while the message is redelivered elsewhere.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type TranscodeRequested struct {
	VideoId string `json:"video_id"`
	Minutes int    `json:"minutes"`
}

// increment on breaking changes
func (TranscodeRequested) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := vulkan.LifecycleContext(nil)
	defer stop()

	pool, err := datastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	ds, err := datastore.NewPostgresDatastore(ctx, pool, nil)
	if err != nil {
		return err
	}

	client, err := vulkan.NewClient(ds, nil)
	if err != nil {
		return err
	}
	transcodes, err := client.RegisterConsumer[TranscodeRequested](ctx, "transcoder", "videos.transcode", &vulkan.ConsumerConfig{
		Message:    &vulkan.MessageOptions{Timeout: 2 * time.Minute},
		MessageMax: &vulkan.MessageOptions{Timeout: time.Hour},
	})
	if err != nil {
		return err
	}

	return transcodes.Consume(ctx, func(ctx context.Context, request *TranscodeRequested) error {
		for minute := range request.Minutes {
			// wanted: "still working, extend my lease" -- nothing to call.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Minute):
				fmt.Printf("%s: minute %d\n", request.VideoId, minute+1)
			}
		}
		return nil
	}, nil)
}
