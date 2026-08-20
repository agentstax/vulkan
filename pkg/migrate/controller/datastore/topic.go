package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (d *MigrateDatastore) ListTopics(ctx context.Context, conn *pgxpool.Conn) ([]*common.Owner, error) {
	rows, err := conn.Query(ctx, `-- vulkan: migrate.ListTopics
SELECT id, system_id, name FROM topic ORDER BY id;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []*common.Owner
	for rows.Next() {
		var id int64
		var systemId int64
		var name string
		if err := rows.Scan(&id, &systemId, &name); err != nil {
			return nil, err
		}
		owner, err := common.NewTopicOwner(systemId, id, name)
		if err != nil {
			return nil, err
		}
		topics = append(topics, owner)
	}
	return topics, rows.Err()
}
