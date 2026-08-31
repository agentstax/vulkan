package datastore

import "github.com/jackc/pgx/v5/pgtype"

// KeyLeaseVerdict classifies a Claim attempt.
type KeyLeaseVerdict string

const (
	KeyLeaseAcquired   KeyLeaseVerdict = "acquired"   // the caller holds the key until release or expiry
	KeyLeaseBusy       KeyLeaseVerdict = "busy"       // another delivery holds the key
	KeyLeaseSuperseded KeyLeaseVerdict = "superseded" // the compacted message is no longer its key's compaction head -- never run it
)

// KeyLease is one Claim outcome. Token is set only when
// acquired; Release matches on it.
type KeyLease struct {
	Verdict         KeyLeaseVerdict
	TopicId         int64
	ConsumerGroupId int64
	MessageKey      string
	Token           pgtype.UUID
}
