package datastore

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	iTopic "github.com/agentstax/vulkan/internal/topic"
)

// CronJobRequests is the job's newest limit requests, one row per
// (request, consumer group that receives it), newest request first.
func (d *CronJobDatastore) CronJobRequests(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string, limit int) ([]*JobRequestStatusData, error) {
	var requests []*JobRequestStatusData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		requests, err = d.cronJobRequests(ctx, jobRequestsTopicId, cronJobId, name, limit)
		return err
	})
	return requests, err
}

func (d *CronJobDatastore) cronJobRequests(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string, limit int) ([]*JobRequestStatusData, error) {
	compactionKey := strconv.FormatInt(cronJobId, 10)

	groups, err := d.matchingGroups(ctx, jobRequestsTopicId, name)
	if err != nil {
		return nil, err
	}
	messages, err := d.jobMessages(ctx, jobRequestsTopicId, compactionKey, limit)
	if err != nil {
		return nil, err
	}
	headId, err := d.headId(ctx, jobRequestsTopicId, compactionKey)
	if err != nil {
		return nil, err
	}

	ids := messageIds(messages)
	var statuses []*JobRequestStatusData
	for _, group := range groups {
		outcomes, err := d.requestOutcomes(ctx, jobRequestsTopicId, group.Id, ids)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, groupJobRequestStatuses(group, messages, headId, outcomes)...)
	}

	// newest request first, groups alphabetical within a request
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].MessageId != statuses[j].MessageId {
			return statuses[i].MessageId > statuses[j].MessageId
		}
		return statuses[i].ConsumerGroup < statuses[j].ConsumerGroup
	})
	return statuses, nil
}

// groupJobRequestStatuses is one consumer group's row per request, newest
// request first.
func groupJobRequestStatuses(group *matchingGroupData, messages []*jobMessageData, headId int64, outcomes map[int64]requestOutcomeData) []*JobRequestStatusData {
	var statuses []*JobRequestStatusData
	for i, message := range messages {

		outcome := outcomes[message.Id]
		status := &JobRequestStatusData{
			ConsumerGroup: group.Name,
			MessageId:     message.Id,
			Payload:       message.Payload,
			ProducedAt:    message.CreatedAt,
			Head:          message.Id == headId,
			Succeeded:     outcome.Succeeded,
			Raised:        outcome.Raised,
			Deferred:      outcome.Deferred,
		}

		// a request that never ran and is not the head was superseded;
		// messages is newest first, so messages[i-1] is what replaced it
		if !outcome.Succeeded && !outcome.Raised && message.Id != headId && i > 0 {
			status.SupersededBy = &messages[i-1].Id
			status.SupersededAt = &messages[i-1].CreatedAt
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// jobMessages is the newest limit message-log rows on the job's compaction
// key, newest first.
func (d *CronJobDatastore) jobMessages(ctx context.Context, jobRequestsTopicId int64, compactionKey string, limit int) ([]*jobMessageData, error) {
	sql := fmt.Sprintf(`
		SELECT m.id, m.payload, m.created_at
		FROM %s m
		WHERE m.compaction_key = $1
		ORDER BY m.id DESC
		LIMIT $2;
	`, iTopic.MessageLogTable(jobRequestsTopicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, compactionKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*jobMessageData
	for rows.Next() {
		var message jobMessageData
		if err := rows.Scan(&message.Id, &message.Payload, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, &message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func messageIds(messages []*jobMessageData) []int64 {
	ids := make([]int64, len(messages))
	for i, message := range messages {
		ids[i] = message.Id
	}
	return ids
}
