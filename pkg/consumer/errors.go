package consumer

// appended to errors.ErrLifecycleContextNotCancellable at the Register call site
const lifecycleContextHelp = `
Register's context is the consumer's lifetime -- cancelling it starts graceful
shutdown, and context.Background/TODO can never be cancelled.

Pass your application's shutdown context:

    ctx, stop := vulkanctx.LifecycleContext() // github.com/agentstax/vulkan/pkg/context
    defer stop()

Or declare that Consume's own ctx is the only off-switch:

    &consumer.MessageConsumerConfig{DisableGracefulShutdown: true}`
