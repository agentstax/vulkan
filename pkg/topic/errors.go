package topic

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrTopicConfigMismatch means Register was called with a PartitionSize the topic wasn't created with.
// Every other config field can be changed by registering again.
var ErrTopicConfigMismatch = diagnostic.NewDiagnosticError("VK0004", diagnostic.RecoveryPermanent,
	"topic partition size does not match the existing topic",
	"register with PartitionSize {existing_partition_size}, or use a new topic name")

// ErrTopicNotFound means the named topic has no row.
//
// Diagnose queries: vulkan explain VK0005
var ErrTopicNotFound = diagnostic.NewDiagnosticError("VK0005", diagnostic.RecoveryPermanent,
	"topic not found",
	"register it with Client.Topic(name).Register first").
	Diagnose(
		diagnostic.NewDiagnosticQuery("the topic row under this name", `
SELECT id, name, created_at FROM {schema}.topic_config WHERE name = '{topic}';`),
		diagnostic.NewDiagnosticQuery("the topic row behind an id, if that is what the line carried", `
SELECT id, name, created_at FROM {schema}.topic_config WHERE id = {topic_id};`),
		diagnostic.NewDiagnosticQuery("every registered topic, if the name itself is wrong", `
SELECT name FROM {schema}.topic_config ORDER BY name;`),
	)

// ErrTopicNotEmpty means Destroy was called on a topic that still holds
// messages, without an explicit force override.
//
// Diagnose queries: vulkan explain VK0006
var ErrTopicNotEmpty = diagnostic.NewDiagnosticError("VK0006", diagnostic.RecoveryPermanent,
	"topic still holds messages",
	"pass DestroyOptions.Force to destroy them with the topic").
	Diagnose(
		diagnostic.NewDiagnosticQuery("how many messages the destroy would discard", `
SELECT count(*) AS message_count FROM {schema}.message_log_{topic_id};`),
		diagnostic.NewDiagnosticQuery("the newest of them, to judge whether the topic is still in use", `
SELECT
	id,
	routing_key,
	created_at
FROM {schema}.message_log_{topic_id}
ORDER BY id DESC
LIMIT 20;`),
	)

// ErrTopicNameTaken means Rename's target name already belongs to another topic.
var ErrTopicNameTaken = diagnostic.NewDiagnosticError("VK0007", diagnostic.RecoveryPermanent,
	"topic name already taken",
	"choose a different name")

// ErrTopicPartitionsRemain means Destroy kept finding new partitions after
// its drop-pass limit -- a producer is likely still writing.
//
// Diagnose queries: vulkan explain VK0020
var ErrTopicPartitionsRemain = diagnostic.NewDiagnosticError("VK0020", diagnostic.RecoveryPermanent,
	"topic partitions remain after draining",
	"stop the topic's producers and call DestroyTopic again").
	Diagnose(
		diagnostic.NewDiagnosticQuery("the partitions still attached to the log", `
SELECT partition.relname AS partition
FROM pg_inherits
JOIN pg_class AS partition ON partition.oid = pg_inherits.inhrelid
WHERE pg_inherits.inhparent = to_regclass('{schema}.message_log_{topic_id}')
ORDER BY partition.relname;`),
		diagnostic.NewDiagnosticQuery("whether a producer is still writing -- run it twice", `
SELECT max(id) AS head, count(*) AS message_count FROM {schema}.message_log_{topic_id};`),
	)

// ErrTopicDeclarationInterrupted means the topic row was destroyed between
// the declaration's config write and its re-read; an unchanged retry
// registers the topic fresh, so DatastoreRetry heals the race.
var ErrTopicDeclarationInterrupted = diagnostic.NewDiagnosticError("VK0021", diagnostic.RecoveryTransient,
	"could not finish the topic declaration",
	"run RegisterTopic again if the topic should still exist")

// ErrDestroyDisabled means a Destroy* call ran without AllowDestroy set
// on the admin's config.
var ErrDestroyDisabled = diagnostic.NewDiagnosticError("VK0008", diagnostic.RecoveryPermanent,
	"destroy is disabled",
	"set ClientConfig.AllowDestroy")

// ErrReservedTopicName means Register/Rename touched a name under
// SystemTopicPrefix -- reserved for the admin's own system topics.
var ErrReservedTopicName = diagnostic.NewDiagnosticError("VK0009", diagnostic.RecoveryPermanent,
	"topic name uses the reserved __system. prefix",
	"choose a name outside the __system. prefix")
