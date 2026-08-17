package datastore

// sweptRow is sweepBatch's RETURNING shape -- CompactionKey only exists
// to tell whether the compaction_head cleanup is worth running at all.
type sweptRow struct {
	Id            int64   `db:"id"`
	CompactionKey *string `db:"compaction_key"`
}
