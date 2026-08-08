package consumer

// appended to errors.ErrLifecycleContextNotCancellable at the Consume call site
const lifecycleContextHelp = `
Consume's context is the instance's lifetime -- cancelling it starts graceful
shutdown, and context.Background/TODO can never be cancelled.

Pass your application's shutdown context:

    ctx, stop := vulkanctx.LifecycleContext(nil) // github.com/agentstax/vulkan/pkg/context
    defer stop()

Or declare a consumer that only stops with the process:

    &consumer.ConsumerConfig{DisableGracefulShutdown: true}`
