package compaction

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrCompactionHeadNotFound means no message produced under the key opted
// into compaction, so the key has no compaction_head row.
//
// Diagnose queries: vulkan explain VK0066
var ErrCompactionHeadNotFound = diagnostic.NewDiagnosticError("VK0066", diagnostic.RecoveryPermanent,
	"compaction head not found",
	"produce under message key \"{message_key}\" on topic \"{topic}\" with CompactionOptions.Enable set").
	Diagnose(
		diagnostic.NewDiagnosticQuery("the key's compaction_head row, if one exists", `
SELECT
	compaction_key,
	head_id,
	schema_version,
	compaction_rank
FROM {schema}.compaction_head_{topic_id}
WHERE compaction_key = '{message_key}';`),
		diagnostic.NewDiagnosticQuery("the messages produced under the key -- a NULL compaction_rank never opted into compaction", `
SELECT
	id,
	schema_version,
	compaction_rank,
	created_at
FROM {schema}.message_log_{topic_id}
WHERE message_key = '{message_key}'
ORDER BY id DESC
LIMIT 20;`),
	)
