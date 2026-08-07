package datastore

import "github.com/jackc/pgx/v5/pgtype"

// KeyLeaseVerdict classifies a ClaimKeyLease attempt.
type KeyLeaseVerdict string

const (
	KeyLeaseAcquired   KeyLeaseVerdict = "acquired"   // the caller holds the key until release or expiry
	KeyLeaseBusy       KeyLeaseVerdict = "busy"       // another delivery holds the key
	KeyLeaseSuperseded KeyLeaseVerdict = "superseded" // the message is no longer its key's compaction head -- never run it
)

// KeyLeaseData is one ClaimKeyLease outcome. Token is set only when
// acquired; ReleaseKeyLease matches on it.
type KeyLeaseData struct {
	Verdict         KeyLeaseVerdict
	ConsumerGroupId int64
	CompactionKey   string
	Token           pgtype.UUID
}
