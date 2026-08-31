package consumer

// appended to errors.ErrLifecycleContextNotCancellable at the Consume call site
const lifecycleContextHelp = `
Consume's context is the instance's lifetime -- cancelling it starts graceful
shutdown, and context.Background/TODO can never be cancelled.

Pass your application's shutdown context:

    ctx, stop := vulkan.LifecycleContext(nil) // github.com/agentstax/vulkan/pkg/vulkan
    defer stop()

Or run a session that only stops with the process:

    instance.Consume(ctx, handler, &vulkan.ConsumeOptions{DisableGracefulShutdown: true})`
