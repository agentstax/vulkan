package vulkan

// Every type a user spells lives in this package; each alias points at the
// package that owns it.

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	"github.com/agentstax/vulkan/pkg/scheduler"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
)

// Versioned is the constraint every payload type satisfies by declaring
// its own schema version.
type Versioned = topic.Versioned

type (
	ConsumerInstance[Message Versioned] = consumer.ConsumerInstance[Message]
	ConsumeOptions                      = consumer.ConsumeOptions

	ProducerInstance[Message Versioned] = producer.ProducerInstance[Message]
	ProduceOptions                      = producer.ProduceOptions
	ProduceResult[Message Versioned]    = producer.ProduceResult[Message]
	ProduceItem[Message Versioned]      = producer.ProduceItem[Message]

	SchedulerInstance[Message Versioned] = scheduler.SchedulerInstance[Message]
	CompactionOptions                    = producer.CompactionOptions
	CursorPosition                       = consumergroup.CursorPosition
	ScheduleConfig                       = schedulecontroller.ScheduleConfig

	TopicConfig          = topiccontroller.TopicConfig
	DestroyOptions       = admin.DestroyOptions
	RegisterSystemConfig = admin.RegisterSystemConfig
	RunScheduleConfig    = admin.RunScheduleConfig

	MessageOptions = common.MessageOptions
	RetryPolicy    = common.RetryPolicy

	TopicData                      = topic.TopicData
	ScheduleData                   = schedule.ScheduleData
	GroupData                      = consumergroup.GroupData
	WorkerData                     = worker.WorkerData
	Declaration                    = consumergroup.Declaration
	MessageMeta                    = consumergroup.MessageMeta
	MessageData[Message Versioned] = producer.MessageData[Message]
	VersionHealth                  = admin.VersionHealth
	TopicSnapshot                  = metrics.TopicSnapshot
	SystemData                     = system.SystemData
	GroupStatus                    = schedule.GroupStatus
	MessageStatus                  = schedule.MessageStatus
	StoredMessage                  = schedule.StoredMessage
)

// ConcurrencyPolicy is a message's concurrency policy.
type ConcurrencyPolicy = common.ConcurrencyPolicy

const (
	ConcurrencyParallel  = common.ConcurrencyParallel
	ConcurrencyExclusive = common.ConcurrencyExclusive
	ConcurrencyOrdered   = common.ConcurrencyOrdered
)

// LifecycleContext wires SIGINT/SIGTERM into the returned context --
// the cancellable context Consume requires.
func LifecycleContext(log logging.Logger) (context.Context, context.CancelFunc) {
	return common.LifecycleContext(log)
}

// MetaFromContext reads the delivery's message metadata inside a handler.
func MetaFromContext(ctx context.Context) (MessageMeta, bool) {
	return consumergroup.MetaFromContext(ctx)
}

// Terminal marks a handler error as never retryable -- the delivery goes
// straight to dead.
func Terminal(cause error) error {
	return consumergroup.Terminal(cause)
}

// Delay asks for the message to run again after delay, without counting a
// failure.
func Delay(delay time.Duration) error {
	return consumergroup.Delay(delay)
}

// NewProduceItem pairs one message with its options for a multi-message
// produce call. options may be nil for the defaults.
func NewProduceItem[Message Versioned](message *Message, options *ProduceOptions) (*ProduceItem[Message], error) {
	return producer.NewProduceItem[Message](message, options)
}

// NewCompactionOptions enables compaction for a produced message at rank.
func NewCompactionOptions(rank int64) (*CompactionOptions, error) {
	return producer.NewCompactionOptions(rank)
}

// Beginning positions a new group's cursor at the oldest retained message.
func Beginning() CursorPosition {
	return consumergroup.Beginning()
}

// Head positions a new group's cursor at MAX(id) of the message log when
// the cursor row is written.
func Head() CursorPosition {
	return consumergroup.Head()
}
