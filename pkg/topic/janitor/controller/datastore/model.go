package datastore

// sweptRow is sweepBatch's RETURNING shape -- CompactionRank only exists
// to tell whether the compaction_head cleanup is worth running at all.
type sweptRow struct {
	Id             int64  `db:"id"`
	CompactionRank *int64 `db:"compaction_rank"`
}
