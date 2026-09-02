package topic

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventTopicConfigReplaced means a declaration overwrote a topic row's
// differing mutable config -- two declarers disagree about the topic.
//
// Diagnose queries: vulkan explain VK0061
var EventTopicConfigReplaced = diagnostic.NewEvent("VK0061",
	"topic config replaced",
	"the newest declaration wins; if this is unexpected or repeats on every restart, two services declare this topic with different configs and overwrite each other").
	Diagnose(
		diagnostic.NewQuery("every declaration this topic has received, newest first", `
SELECT
	name,
	retention_ttl_ns,
	allow_drop_past_committed,
	idempotency_key_ttl_ns,
	delivery_log_mode,
	declared_by,
	declared_at
FROM {schema}.topic_config_log
WHERE topic_id = {topic_id}
ORDER BY id DESC
LIMIT 10;`),
	)
