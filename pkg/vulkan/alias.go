package vulkan

// Every type a user spells through this package is an alias into the
// package that declares it, so a click-through lands on the declaration.

import (
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/produce/batcher"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
)

type (
	Versioned                      = common.Versioned
	MessageData[Message Versioned] = common.MessageData[Message]
	MessageOptions                 = common.MessageOptions
	RetryPolicy                    = common.RetryPolicy
	ConcurrencyPolicy              = common.ConcurrencyPolicy
	Owner                          = common.Owner
	OwnerKind                      = common.OwnerKind
	Logger                         = logging.Logger
	DiagnosticError                = diagnostic.DiagnosticError
	DiagnosticEvent                = diagnostic.DiagnosticEvent
	DiagnosticQuery                = diagnostic.DiagnosticQuery
	DiagnosticRecovery             = diagnostic.DiagnosticRecovery
	DiagnosticKind                 = diagnostic.DiagnosticKind

	PostgresDatastore = datastore.PostgresDatastore
	Querier           = datastore.Querier
	Tx                = datastore.Tx
	TransactionFunc   = datastore.TransactionFunc

	ProduceOptions                   = produce.ProduceOptions
	CompactionOptions                = produce.CompactionOptions
	ProducerFunc[Message Versioned]  = produce.ProducerFunc[Message]
	BatcherConfig                    = batcher.BatcherConfig
	ProducerConfig                   = producer.ProducerConfig
	ProduceItem[Message Versioned]   = producer.ProduceItem[Message]
	ProduceResult[Message Versioned] = producer.ProduceResult[Message]

	ConsumerConfig                  = consumer.ConsumerConfig
	ConsumeOptions                  = consumer.ConsumeOptions
	ConsumerFunc[Message Versioned] = consumer.ConsumerFunc[Message]
	CursorPosition                  = consume.CursorPosition
	CursorPositionKind              = consume.CursorPositionKind
	GroupData                       = consume.GroupData
	BindingDeclaration              = consume.BindingDeclaration
	BindingOutcome                  = consume.BindingOutcome
	MessageMeta                     = consume.MessageMeta

	TopicConfig     = topic.TopicConfig
	TopicData       = topic.TopicData
	DeliveryLogMode = topic.DeliveryLogMode

	ScheduleSpec   = schedule.ScheduleSpec
	ScheduleConfig = schedule.ScheduleConfig
	ScheduleData   = schedule.ScheduleData
	GroupStatus    = schedule.GroupStatus
	MessageStatus  = schedule.MessageStatus
	MessageOutcome = schedule.MessageOutcome
	StoredMessage  = schedule.StoredMessage

	SystemConfig   = system.SystemConfig
	SystemData     = system.SystemData
	WorkerData     = worker.WorkerData
	InstanceTarget = worker.InstanceTarget

	DestroyOptions       = admin.DestroyOptions
	RegisterSystemConfig = admin.RegisterSystemConfig
	RunScheduleConfig    = admin.RunScheduleConfig
	VersionHealth        = admin.VersionHealth

	PartitionCountJobConfig     = alert.PartitionCountJobConfig
	CompactionReadCostJobConfig = alert.CompactionReadCostJobConfig
	WorkerLivenessJobConfig     = alert.WorkerLivenessJobConfig

	TopicSnapshot            = metrics.TopicSnapshot
	ConsumerGroupSnapshot    = metrics.ConsumerGroupSnapshot
	SchemaVersionSnapshot    = metrics.SchemaVersionSnapshot
	GroupSchemaVersionLag    = metrics.GroupSchemaVersionLag
	GroupLag                 = metrics.GroupLag
	CursorSnapshot           = metrics.CursorSnapshot
	ExceptionSnapshot        = metrics.ExceptionSnapshot
	AbandonedRoutineSnapshot = metrics.AbandonedRoutineSnapshot
	Measurement              = metrics.Measurement
	MetricKind               = metrics.MetricKind
	MetricUnit               = metrics.MetricUnit
	Alert                    = alert.Alert
	AlertStatus              = alert.AlertStatus
	AlertSeverity            = alert.AlertSeverity
)

const (
	RecoveryTransient = diagnostic.RecoveryTransient
	RecoveryPermanent = diagnostic.RecoveryPermanent

	DiagnosticKindError  = diagnostic.DiagnosticKindError
	DiagnosticKindEvent  = diagnostic.DiagnosticKindEvent
	DiagnosticKindMetric = diagnostic.DiagnosticKindMetric

	ConcurrencyParallel  = common.ConcurrencyParallel
	ConcurrencyExclusive = common.ConcurrencyExclusive
	ConcurrencyOrdered   = common.ConcurrencyOrdered

	DeliveryLogModeOff      = topic.DeliveryLogModeOff
	DeliveryLogModeFailures = topic.DeliveryLogModeFailures
	DeliveryLogModeAll      = topic.DeliveryLogModeAll

	OwnerAny           = common.OwnerAny
	OwnerSystem        = common.OwnerSystem
	OwnerTopic         = common.OwnerTopic
	OwnerConsumerGroup = common.OwnerConsumerGroup

	CursorPositionBeginning = consume.CursorPositionBeginning
	CursorPositionHead      = consume.CursorPositionHead
	BindingInstalled        = consume.BindingInstalled
	BindingJoined           = consume.BindingJoined
	BindingWaiting          = consume.BindingWaiting

	MessagePending    = schedule.MessagePending
	MessageDeferred   = schedule.MessageDeferred
	MessageSucceeded  = schedule.MessageSucceeded
	MessageFailed     = schedule.MessageFailed
	MessageSuperseded = schedule.MessageSuperseded

	NoInstanceTarget = worker.NoInstanceTarget

	MetricKindCounter      = metrics.MetricKindCounter
	MetricKindGauge        = metrics.MetricKindGauge
	MetricUnitMilliseconds = metrics.MetricUnitMilliseconds
	AlertStatusActive      = alert.AlertStatusActive
	AlertStatusResolved    = alert.AlertStatusResolved
	AlertSeverityWarn      = alert.AlertSeverityWarn
)

var (
	LifecycleContext     = common.LifecycleContext
	MetaFromContext      = consume.MetaFromContext
	Terminal             = consume.Terminal
	Delay                = consume.Delay
	Beginning            = consume.Beginning
	Head                 = consume.Head
	NewCompactionOptions = produce.NewCompactionOptions
)
