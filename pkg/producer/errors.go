package producer

// appended to errors.ErrLifecycleContextNotCancellable at the Register call site
const lifecycleContextHelp = `
Register's context is the producer's lifetime -- cancelling it starts graceful
shutdown, and context.Background/TODO can never be cancelled.

Pass your application's shutdown context:

    ctx, stop := vulkanctx.LifecycleContext(nil) // github.com/agentstax/vulkan/pkg/context
    defer stop()

Or declare a short-lived producer fire-and-forget:

    &producer.MessageProducerConfig{DisableGracefulShutdown: true}`
