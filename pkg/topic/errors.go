package topic

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrTopicConfigMismatch means Register was called with a PartitionSize the topic wasn't created with.
// Every other config field can be changed by registering again.
var ErrTopicConfigMismatch = diagnostic.NewError("VK0004", diagnostic.Permanent,
	"topic partition size does not match the existing topic",
	"register with PartitionSize {existing_partition_size}, or use a new topic name")

// ErrTopicNotFound means the named topic has no row.
//
// Diagnose queries: vulkan explain VK0005
var ErrTopicNotFound = diagnostic.NewError("VK0005", diagnostic.Permanent,
	"topic not found",
	"register it with Client.RegisterTopic first").
	Diagnose(
		diagnostic.NewQuery("the topic row under this name", `
SELECT id, name, created_at FROM topic_config WHERE name = '{topic}';`),
		diagnostic.NewQuery("the topic row behind an id, if that is what the line carried", `
SELECT id, name, created_at FROM topic_config WHERE id = {topic_id};`),
		diagnostic.NewQuery("every registered topic, if the name itself is wrong", `
SELECT name FROM topic_config ORDER BY name;`),
	)

// ErrTopicNotEmpty means Destroy was called on a topic that still holds
// messages, without an explicit force override.
//
// Diagnose queries: vulkan explain VK0006
var ErrTopicNotEmpty = diagnostic.NewError("VK0006", diagnostic.Permanent,
	"topic still holds messages",
	"pass DestroyOptions.Force to destroy them with the topic").
	Diagnose(
		diagnostic.NewQuery("how many messages the destroy would discard", `
SELECT count(*) AS message_count FROM message_log_{topic_id};`),
		diagnostic.NewQuery("the newest of them, to judge whether the topic is still in use", `
SELECT
	id,
	routing_key,
	created_at
FROM message_log_{topic_id}
ORDER BY id DESC
LIMIT 20;`),
	)

// ErrTopicNameTaken means Rename's target name already belongs to another topic.
var ErrTopicNameTaken = diagnostic.NewError("VK0007", diagnostic.Permanent,
	"topic name already taken",
	"choose a different name")

// ErrTopicPartitionsRemain means Destroy kept finding new partitions after
// its drop-pass limit -- a producer is likely still writing.
//
// Diagnose queries: vulkan explain VK0020
var ErrTopicPartitionsRemain = diagnostic.NewError("VK0020", diagnostic.Permanent,
	"topic partitions remain after draining",
	"stop the topic's producers and call DestroyTopic again").
	Diagnose(
		diagnostic.NewQuery("the partitions still attached to the log", `
SELECT partition.relname AS partition
FROM pg_inherits
JOIN pg_class AS partition ON partition.oid = pg_inherits.inhrelid
WHERE pg_inherits.inhparent = to_regclass('message_log_{topic_id}')
ORDER BY partition.relname;`),
		diagnostic.NewQuery("whether a producer is still writing -- run it twice", `
SELECT max(id) AS head, count(*) AS message_count FROM message_log_{topic_id};`),
	)

// ErrTopicDeclarationInterrupted means the topic row was destroyed between
// the declaration's config write and its re-read; an unchanged retry
// registers the topic fresh, so DatastoreRetry heals the race.
var ErrTopicDeclarationInterrupted = diagnostic.NewError("VK0021", diagnostic.Transient,
	"could not finish the topic declaration",
	"run RegisterTopic again if the topic should still exist")

// ErrDestroyDisabled means a Destroy* call ran without AllowDestroy set
// on the admin's config.
var ErrDestroyDisabled = diagnostic.NewError("VK0008", diagnostic.Permanent,
	"destroy is disabled",
	"set ClientConfig.AllowDestroy")

// ErrReservedTopicName means Register/Rename touched a name under
// SystemTopicPrefix -- reserved for the admin's own system topics.
var ErrReservedTopicName = diagnostic.NewError("VK0009", diagnostic.Permanent,
	"topic name uses the reserved __system. prefix",
	"choose a name outside the __system. prefix")
