package datastore

import (
	"context"
	"fmt"
	"sort"

	"github.com/agentstax/vulkan/pkg/topic"
)

// ListMessages is the schedule's newest limit messages, one row per
// (message, consumer group that receives it), newest message first.
func (d *ScheduleDatastore) ListMessages(ctx context.Context, topicId int64, name string, limit int) ([]ScheduleMessageStatusRow, error) {
	var requests []ScheduleMessageStatusRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		requests, err = d.listMessages(ctx, topicId, name, limit)
		return err
	})
	return requests, err
}

func (d *ScheduleDatastore) listMessages(ctx context.Context, topicId int64, name string, limit int) ([]ScheduleMessageStatusRow, error) {
	groups, err := d.matchingGroups(ctx, topicId, name)
	if err != nil {
		return nil, err
	}
	messages, err := d.keyMessages(ctx, topicId, name, limit)
	if err != nil {
		return nil, err
	}
	headId, err := d.headId(ctx, topicId, name)
	if err != nil {
		return nil, err
	}

	ids := messageIds(messages)
	var statuses []ScheduleMessageStatusRow
	for _, group := range groups {
		outcomes, err := d.messageOutcomes(ctx, topicId, group.Id, ids)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, groupMessageStatuses(group, messages, headId, outcomes)...)
	}

	// newest message first, groups alphabetical within a message
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].MessageId != statuses[j].MessageId {
			return statuses[i].MessageId > statuses[j].MessageId
		}
		return statuses[i].ConsumerGroup < statuses[j].ConsumerGroup
	})
	return statuses, nil
}

// keyMessages is the newest limit message-log rows on the schedule's message
// key, newest first.
func (d *ScheduleDatastore) keyMessages(ctx context.Context, topicId int64, name string, limit int) ([]keyMessageRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: schedule.keyMessages
		SELECT
			m.id,
			(m.options->>'scheduled_at')::timestamptz,
			m.created_at
		FROM %[1]s.%[2]s m
		WHERE m.message_key = $1
		ORDER BY m.id DESC
		LIMIT $2;
	`, d.Datastore.Schema, topic.MessageLogTable(topicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []keyMessageRow
	for rows.Next() {
		var message keyMessageRow
		if err := rows.Scan(&message.Id, &message.ScheduledAt, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

// ***************
// *** HELPERS ***
// ***************

// groupMessageStatuses is one consumer group's row per message, newest
// message first.
func groupMessageStatuses(group matchingGroupRow, messages []keyMessageRow, headId int64, outcomes map[int64]messageOutcomeRow) []ScheduleMessageStatusRow {
	var statuses []ScheduleMessageStatusRow
	for i, message := range messages {
		outcome := outcomes[message.Id]
		status := ScheduleMessageStatusRow{
			ConsumerGroup: group.Name,
			MessageId:     message.Id,
			ScheduledAt:   message.ScheduledAt,
			ProducedAt:    message.CreatedAt,
			Head:          message.Id == headId,
			Succeeded:     outcome.Succeeded,
			Raised:        outcome.Raised,
			Deferred:      outcome.Deferred,
		}

		// a message that never ran and is not the head was superseded;
		// messages is newest first, so messages[i-1] is what replaced it
		if !outcome.Succeeded && !outcome.Raised && message.Id != headId && i > 0 {
			status.SupersededBy = &messages[i-1].Id
			status.SupersededAt = &messages[i-1].CreatedAt
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func messageIds(messages []keyMessageRow) []int64 {
	ids := make([]int64, len(messages))
	for i, message := range messages {
		ids[i] = message.Id
	}
	return ids
}
