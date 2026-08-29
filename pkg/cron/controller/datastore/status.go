package datastore

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// Status is one GroupStatusData per consumer group that receives the
// job's requests. Counts cover the topic's retention window.
func (d *CronJobDatastore) Status(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string) ([]GroupStatusData, error) {
	var statuses []GroupStatusData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		statuses, err = d.status(ctx, jobRequestsTopicId, cronJobId, name)
		return err
	})
	return statuses, err
}

func (d *CronJobDatastore) status(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string) ([]GroupStatusData, error) {
	compactionKey := strconv.FormatInt(cronJobId, 10)

	groups, err := d.matchingGroups(ctx, jobRequestsTopicId, name)
	if err != nil {
		return nil, err
	}
	messageIds, err := d.jobMessageIds(ctx, jobRequestsTopicId, compactionKey)
	if err != nil {
		return nil, err
	}
	headId, err := d.headId(ctx, jobRequestsTopicId, compactionKey)
	if err != nil {
		return nil, err
	}

	var statuses []GroupStatusData
	for _, group := range groups {
		outcomes, err := d.requestOutcomes(ctx, jobRequestsTopicId, group.Id, messageIds)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, groupStatus(group, messageIds, headId, outcomes))
	}
	return statuses, nil
}

// matchingGroups is every consumer group that receives the job's requests,
// ordered by name.
func (d *CronJobDatastore) matchingGroups(ctx context.Context, jobRequestsTopicId int64, name string) ([]matchingGroupData, error) {
	sql := fmt.Sprintf(`
		-- vulkan: cron.matchingGroups
		SELECT cg.id, cg.name
		FROM consumer_group_config cg
		WHERE cg.topic_id = $1
		  AND (
			-- a group with no bindings receives every routing key
			NOT EXISTS (SELECT 1 FROM %[1]s b WHERE b.consumer_group_id = cg.id)
			-- otherwise a binding must match the job's name ($2)
			OR EXISTS (SELECT 1 FROM %[1]s b WHERE b.consumer_group_id = cg.id AND $2 ~ b.pattern_regex)
		  )
		ORDER BY cg.name;
	`, iTopic.BindingConfigTable(jobRequestsTopicId))
	rows, err := d.Datastore.Pool.Query(ctx, sql, jobRequestsTopicId, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []matchingGroupData
	for rows.Next() {
		var group matchingGroupData
		if err := rows.Scan(&group.Id, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

// jobMessageIds is every message id on the job's compaction key still inside
// the retention window.
func (d *CronJobDatastore) jobMessageIds(ctx context.Context, jobRequestsTopicId int64, compactionKey string) ([]int64, error) {
	sql := fmt.Sprintf(`
		-- vulkan: cron.jobMessageIds
		SELECT m.id
		FROM %s m
		WHERE m.compaction_key = $1;
	`, iTopic.MessageLogTable(jobRequestsTopicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, compactionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messageIds []int64
	for rows.Next() {
		var messageId int64
		if err := rows.Scan(&messageId); err != nil {
			return nil, err
		}
		messageIds = append(messageIds, messageId)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messageIds, nil
}

// headId is the key's compaction_head pointer; 0 when the job has no messages.
func (d *CronJobDatastore) headId(ctx context.Context, jobRequestsTopicId int64, compactionKey string) (int64, error) {
	sql := fmt.Sprintf(`
		-- vulkan: cron.headId
		SELECT head_id
		FROM %s
		WHERE compaction_key = $1;
	`, iTopic.CompactionHeadTable(jobRequestsTopicId))

	var headId int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, compactionKey).Scan(&headId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return headId, nil
}

// requestOutcomes is one consumer group's delivery history per message,
// rolled up to booleans and indexed by message id.
func (d *CronJobDatastore) requestOutcomes(ctx context.Context, jobRequestsTopicId int64, consumerGroupId int64, messageIds []int64) (map[int64]requestOutcomeData, error) {
	sql := fmt.Sprintf(`
		-- vulkan: cron.requestOutcomes
		SELECT
			d.message_id,
			bool_or(d.status = 'success')                          AS succeeded,
			bool_or(d.status IN ('failure', 'expired', 'killed'))  AS raised,
			bool_or(d.status = 'deferred')                         AS deferred
		FROM %s d
		WHERE d.consumer_group_id = $1
		  AND d.message_id = ANY($2)
		GROUP BY d.message_id;
	`, iTopic.DeliveryLogTable(jobRequestsTopicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, consumerGroupId, messageIds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	outcomes := make(map[int64]requestOutcomeData)
	for rows.Next() {
		var messageId int64
		var outcome requestOutcomeData
		if err := rows.Scan(&messageId, &outcome.Succeeded, &outcome.Raised, &outcome.Deferred); err != nil {
			return nil, err
		}
		outcomes[messageId] = outcome
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

// ***************
// *** HELPERS ***
// ***************

// 'superseded' and still-pending 'deferred' requests never
// ran, so ran = succeeded + failed always holds
func groupStatus(group matchingGroupData, messageIds []int64, headId int64, outcomes map[int64]requestOutcomeData) GroupStatusData {
	status := GroupStatusData{ConsumerGroup: group.Name}
	for _, messageId := range messageIds {
		outcome := outcomes[messageId]
		switch {
		case outcome.Succeeded:
			status.Ran++
			status.Succeeded++
		case outcome.Raised:
			status.Ran++
			status.Failed++
		// dropped unrun: no longer the key's head and this group never ran it
		case messageId != headId:
			status.Superseded++
		}
	}
	return status
}
