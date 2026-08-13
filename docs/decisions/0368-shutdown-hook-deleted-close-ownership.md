---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0368 — The consumer shutdown hook is deleted; the app that constructs the datastore closes it

**Context.** `Consume`'s exit ran `DefaultShutdownFunc`, which called `ConsumerDatastore.Shutdown` and closed the shared pgx pool — after any `Consume` returned, every other producer or consumer on that `PostgresDatastore` got "closed pool".

**Decision.** The whole hook was deleted after surveying the field (River, gue, pgmq, Pub/Sub, NATS, franz-go, confluent, sarama, amqp091, Watermill, net/http, gRPC, asynq, Temporal): no library has a resource-owning shutdown callback, and every library taking an injected pool leaves closing it to the app that constructed it. Deleted: `ShutdownFunc`/`DefaultShutdownFunc`/`WithShutdown`/`WithShutdownTimeout`, `ConsumerDatastore.Shutdown`, the dead `Drain(ctx, wg)`, and `Config.ShutdownTimeout` — once the hook died the knob bounded nothing live; wind-down is already capped per message by `WorkTimeout + WorkTimeoutGrace` hard-abandon. `PostgresDatastore.Shutdown()` was renamed `Close()`, matching what `sql.DB`/`pgxpool` call the app-owned teardown.

**Consequences.** Ownership rule for both components identically: the app that constructed the datastore closes it (`defer ds.Close()`); producers and consumers borrow it and never close it. The Go team's rejection of ctx-shutdown for net/http (golang/go#52805) validates the model: their objection is that `ListenAndServe` returns before the drain; `Consume` blocks through the drain and returns nil — the Pub/Sub `Receive`/asynq/Temporal camp.
