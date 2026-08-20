---
title: Error codes
slug: errors
---

Every Vulkan error carries a stable `VK` code. The code renders at the
end of the one-liner (`[VK0005]`), in JSON logs, and in the CLI's error
block; paste the message text or code into search to land on its page.

| Code | Problem | Recovery |
| ---- | ------- | -------- |
| [VK0001](/errors/VK0001) | instance is already consuming | permanent |
| [VK0002](/errors/VK0002) | lifecycle context can never be cancelled | permanent |
| [VK0003](/errors/VK0003) | lease lost to another consumer | permanent |
| [VK0004](/errors/VK0004) | topic partition size does not match the existing topic | permanent |
| [VK0005](/errors/VK0005) | topic not found | permanent |
| [VK0006](/errors/VK0006) | topic still holds messages | permanent |
| [VK0007](/errors/VK0007) | topic name already taken | permanent |
| [VK0008](/errors/VK0008) | destroy is disabled | permanent |
| [VK0009](/errors/VK0009) | topic name uses the reserved __system. prefix | permanent |
| [VK0010](/errors/VK0010) | a worker instance is still live | permanent |
| [VK0011](/errors/VK0011) | topics are still registered | permanent |
| [VK0012](/errors/VK0012) | worker instance row expired or was removed | permanent |
| [VK0013](/errors/VK0013) | cron job not found | permanent |
| [VK0014](/errors/VK0014) | consumer group not found | permanent |
| [VK0015](/errors/VK0015) | consumer group still has a live consumer | permanent |
| [VK0016](/errors/VK0016) | consumer group still has delivery rows | permanent |
| [VK0017](/errors/VK0017) | schema not registered | permanent |
| [VK0018](/errors/VK0018) | could not create the covering partition | transient |
| [VK0019](/errors/VK0019) | commit confirmation was lost | permanent |
| [VK0020](/errors/VK0020) | topic partitions remain after draining | permanent |
| [VK0021](/errors/VK0021) | could not finish the topic declaration | transient |
| [VK0022](/errors/VK0022) | schema version is older than this build requires | permanent |
| [VK0023](/errors/VK0023) | schema version is newer than this build understands | permanent |
| [VK0024](/errors/VK0024) | could not finish the worker declaration | transient |
| [VK0025](/errors/VK0025) | could not finish the cron job declaration | transient |

Log events share the same `VK` code space: a Warn- or Error-level line
that is operator-actionable carries its code in the line's `code` attr,
and the code lands on a page here the same way.

| Code | Event | Level |
| ---- | ----- | ----- |
| [VK0026](/errors/VK0026) | lease reclaimed from expired worker | warn |
| [VK0027](/errors/VK0027) | range quarantined after max reclaims | warn |
| [VK0028](/errors/VK0028) | messages dead-lettered | warn |
| [VK0029](/errors/VK0029) | message dead-lettered | warn |
| [VK0030](/errors/VK0030) | exception dead-lettered | warn |
| [VK0031](/errors/VK0031) | crash-loop kill backstop fired | warn |
| [VK0032](/errors/VK0032) | stored message options outside this consumer's bounds | warn |
| [VK0033](/errors/VK0033) | could not create partition ahead | warn |
| [VK0034](/errors/VK0034) | worker instance lost | warn |
| [VK0035](/errors/VK0035) | manager row suspended | warn |
| [VK0036](/errors/VK0036) | worker tick backoff curve exhausted | error |
| [VK0037](/errors/VK0037) | cron job request was already published by an earlier ambiguous commit | warn |
