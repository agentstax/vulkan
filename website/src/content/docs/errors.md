---
title: Error codes
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
| [VK0053](/errors/VK0053) | could not take a lock needed by the migration step | transient |

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
| [VK0038](/errors/VK0038) | produce exceeded the duration threshold | warn |
| [VK0039](/errors/VK0039) | delivery dispatch exceeded the duration threshold | warn |
| [VK0040](/errors/VK0040) | worker tick exceeded its poll rate | warn |
| [VK0041](/errors/VK0041) | consumer stopped | info |
| [VK0052](/errors/VK0052) | abandoned-routine events dropped | warn |

Declared metrics share the code space too: a measurement's name resolves
to its declaration, and `vulkan explain` accepts the code, the full
name, or the stop-line attr key (`ready_count`).

| Code | Metric | Kind |
| ---- | ------ | ---- |
| [VK0042](/errors/VK0042) | vulkan.consumer.session.claimed | counter |
| [VK0043](/errors/VK0043) | vulkan.consumer.session.success | counter |
| [VK0044](/errors/VK0044) | vulkan.consumer.session.superseded | counter |
| [VK0045](/errors/VK0045) | vulkan.consumer.session.ready | counter |
| [VK0046](/errors/VK0046) | vulkan.consumer.session.deferred | counter |
| [VK0047](/errors/VK0047) | vulkan.consumer.session.dead | counter |
| [VK0048](/errors/VK0048) | vulkan.consumer.session.reclaimed | counter |
| [VK0049](/errors/VK0049) | vulkan.consumer.session.quarantined | counter |
| [VK0050](/errors/VK0050) | vulkan.consumer.session.abandoned | counter |
| [VK0051](/errors/VK0051) | vulkan.consumer.session.lease_lost | counter |
