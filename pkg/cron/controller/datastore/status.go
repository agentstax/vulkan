package datastore

import (
	"context"
	"fmt"
	"strconv"

	iTopic "github.com/agentstax/vulkan/internal/topic"
)

// CronJobStatus is one GroupStatusData per consumer group that receives the
// job's requests. Counts cover the topic's retention window.
func (d *CronJobDatastore) CronJobStatus(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string) ([]*GroupStatusData, error) {
	var statuses []*GroupStatusData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		statuses, err = d.cronJobStatus(ctx, jobRequestsTopicId, cronJobId, name)
		return err
	})
	return statuses, err
}

func (d *CronJobDatastore) cronJobStatus(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string) ([]*GroupStatusData, error) {
	// 'superseded' and still-pending 'deferred' rows never
	// ran, so ran = succeeded + failed always holds
	sql := fmt.Sprintf(`
		WITH matching_group AS (
			SELECT
				cg.id,
				cg.name
			FROM consumer_group cg
			WHERE cg.topic_id = $1
			  AND (
				-- a group with no bindings receives every routing key
				NOT EXISTS (SELECT 1 FROM binding b WHERE b.consumer_group_id = cg.id)
				-- otherwise a binding must match the job's name ($2)
				OR EXISTS (SELECT 1 FROM binding b WHERE b.consumer_group_id = cg.id AND $2 ~ b.pattern)
			  )
		), job_message AS (
			SELECT m.id
			FROM %s m
			WHERE m.compaction_key = $3
		), job_request AS (
			SELECT
				d.consumer_group_id,
				d.message_id,
				bool_or(d.status = 'success')                          AS succeeded,
				bool_or(d.status IN ('failure', 'expired', 'killed'))  AS raised
			FROM %s d
			JOIN %s m ON m.id = d.message_id
			WHERE m.compaction_key = $3
			GROUP BY d.consumer_group_id, d.message_id
		)
		SELECT
			g.name,
			COUNT(r.message_id) FILTER (WHERE r.succeeded OR r.raised)      AS ran,
			COUNT(r.message_id) FILTER (WHERE r.succeeded)                  AS succeeded,
			COUNT(r.message_id) FILTER (WHERE r.raised AND NOT r.succeeded) AS failed,
			-- dropped unrun: no longer the key's head and this group never ran it
			COUNT(jm.id) FILTER (
				WHERE jm.id <> (
					SELECT head_id
					FROM compaction_head
					WHERE topic_id = $1
					  AND compaction_key = $3
				)
				  AND NOT COALESCE(r.succeeded OR r.raised, false)
			) AS superseded
		FROM matching_group g
		LEFT JOIN job_message jm ON true
		LEFT JOIN job_request r ON r.consumer_group_id = g.id AND r.message_id = jm.id
		GROUP BY g.id, g.name
		ORDER BY g.name;
	`, iTopic.MessageLogTable(jobRequestsTopicId), iTopic.DeliveryLogTable(jobRequestsTopicId), iTopic.MessageLogTable(jobRequestsTopicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, jobRequestsTopicId, name, strconv.FormatInt(cronJobId, 10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []*GroupStatusData
	for rows.Next() {
		var status GroupStatusData
		if err := rows.Scan(&status.ConsumerGroup, &status.Ran, &status.Succeeded, &status.Failed, &status.Superseded); err != nil {
			return nil, err
		}
		statuses = append(statuses, &status)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return statuses, nil
}
