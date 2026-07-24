package datastore

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListTopics yields one entity per topic
func (d *MigrateDatastore) ListTopics(ctx context.Context, conn *pgxpool.Conn) ([]Entity, error) {
	rows, err := conn.Query(ctx, `SELECT id, name FROM topic ORDER BY id;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.Id, &e.Name); err != nil {
			return nil, err
		}
		topics = append(topics, e)
	}
	return topics, rows.Err()
}
