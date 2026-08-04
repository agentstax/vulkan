package consumer

// one row per consumer kind -- setting a row's target_instances to 0 suspends
// just that kind's new claims, leaving the others running
const (
	WorkerMessageConsumer   = "message_consumer"
	WorkerExceptionConsumer = "exception_consumer"
	WorkerDeliveryConsumer  = "delivery_consumer"
)

// consumer worker rows carry no tuning -- the runners pace from ConsumerConfig
type consumerWorkerMetadata struct{}

func (m *consumerWorkerMetadata) Validate() error {
	return nil
}
