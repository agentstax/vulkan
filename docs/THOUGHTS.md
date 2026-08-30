# Public API

---
- topic.SchemaVersion(1) ?????
- no generics necessary?
- RegisterCronJob better mirrors producer and consumer (have consumerFunc as handler on cronjob config?? and run is just consume?)
- Public runnables Producer or Consumer or Cronjob etc should also just run system manager
- User side idempotency keys should be string, internals can be UUID

- hide RegisterSystem calls
- improve comments on New*
- where to put consumer.ConsumerConfig ie NewConsumer or Register, same pattern for producer etc
- RegisterCronJob returns
---

01 reconsider having to call register system
- it needs to be called but it doesn't really do anything as it has no config so its always the same call and idempotent

01 our comments for New* funcs are abyssmal. These are the first funcs users see

01 reconsider having topic.SchemaVersion(1)
- only define it on register topic?
- only define on New(Producer/Consumer)
- Also should probably rename to something like MessageSchemaVersion <- this is too long but something that says this is your payload schema
- this one is tough I know why it is there now, but for a second I didn't and its a pretty confusing and random looking param

01 all examples should follow pattern with Message struct *V1 as end to help better encourage schema versioning
- how can we use this with our topic.SchemaVersion(1) in such a way to connect the dots between the two more obviously
  something like topic.Schema(V1)

01 is there anyway we can not do the generic in producer.NewProducer[OrderPlacedV1](ds, nil)
   and just completely infer the type from Produce or Consume? does go 1.27 help?

02 produce-in-tx is nasty it really needs to be cleaned up
- maybe less closures or required params (get meta context like for consumer)
- produce options not required in return

03 looks pretty good

04 should NewConsumer be holding consumer config or should that be on register?
	paymentConsumer, err := consumer.NewConsumer[PaymentRequested](ds, &consumer.ConsumerConfig{
		Message: &common.MessageOptions{
			Timeout: 10 * time.Second,
			Retry:   &common.RetryPolicy{MaxRetries: 3, BaseDelay: 2 * time.Second},
		},
	})

05 ignoring this for now

06 RegisterCronJob should return object such that consumer can use its defined topic and binding on it
- jobConsumer.Register(ctx, "invoice-runner", cron.TopicName, topic.SchemaVersion(1), []string{"invoices.nightly"})
  bad that we have to know to find topic and binding. should just be cronjob.TopicName, cronjob.Bindings

06 RegisterCronJob should likely be more like producer and consumer ie
   NewCronJob -> Register -> Run, such that it could run on its own

07 looks pretty good

08 Need to rethink if Consumer should auto run system manager not just manager
- System manager is a good concept for eventual helm chart deploys but needing to know about that concept now is strange
  and we want things to just 'work'

09 Need to rethink if we want users converting string to uuid or just pass string
- if we do go string direction should store both string version and uuid version in db for easy lookups

10 why do we need the run group with the atomic CAS? feels odd

11 gonna ignore this one for now, we just don't have a good solution yet but have it on roadmap

# Doc

Have specific 'proposal' pages. Like the idea of having an issue and doc proposal page where people can comment and give thoughts etc

# Other

cleanup justfile

look into AGPLv3 liscense

we need a long 1hr repeatable live test:
- spins up db, multi producer and multi consumers (built images)
- producers are variadic - zero load, low load, high load, ddos territory
- produces report at end of system - failed produces, skipped (dropped) messages, retries
- We can build on this overtime but keep it relatively simple at start
- Might make sense to roll this into bench as a correctness or reliability benchmark

need another review to make sure we are not logging any sensitive information like payload

At end we should put up roadmap as github issues to get liked etc

