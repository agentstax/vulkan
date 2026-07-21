package producer

import "errors"

// ErrNotRegistered means a produce call arrived before Register -- the
// producer has no lifecycle to run under yet. Call Register once, with the
// application's lifecycle context, before producing.
var ErrNotRegistered = errors.New("producer not registered")

// ErrShutdownRequested means Register's lifecycle context is cancelled -- the
// producer refuses new work while anything already queued drains and commits.
var ErrShutdownRequested = errors.New("producer shutdown requested")

// ErrAlreadyRegistered means Register ran twice on one producer. A producer
// registers once, and a wound-down producer stays down -- construct a new
// MessageProducer to produce again.
var ErrAlreadyRegistered = errors.New("producer already registered")

// ErrLifecycleContextNotCancellable means Register was given a context that
// can never be cancelled (e.g. context.Background()), so shutdown could never
// be requested. Pass the application's shutdown context, or opt out with
// MessageProducerConfig.DisableGracefulShutdown.
var ErrLifecycleContextNotCancellable = errors.New("lifecycle context can never be cancelled")

// appended to ErrLifecycleContextNotCancellable at the Register call site
const lifecycleContextHelp = `
Register's context is the producer's lifetime -- cancelling it starts graceful
shutdown, and context.Background/TODO can never be cancelled.

Pass your application's shutdown context:

    ctx, stop := vulkanctx.LifecycleContext() // github.com/agentstax/vulkan/pkg/context
    defer stop()

Or declare a short-lived producer fire-and-forget:

    &producer.MessageProducerConfig{DisableGracefulShutdown: true}`
