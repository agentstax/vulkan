package consume

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// MessageMeta is everything about a delivered message besides its payload,
// read inside consumerFunc via MetaFromContext.
type MessageMeta struct {
	Id             int64     `json:"message_id"`
	RoutingKey     string    `json:"routing_key"`
	MessageKey     string    `json:"message_key"`
	CompactionRank int64     `json:"compaction_rank"`
	CreatedAt      time.Time `json:"created_at"`
	ScheduledAt    time.Time `json:"scheduled_at"` // the scheduled time a schedule's message is for; zero on every other message
	Attempts       int       `json:"attempts"`     // runs before this one -- 0 on the first delivery
	Delays         int       `json:"delays"`       // later runs the handler requested so far

	// Options - the resolved MessageOptions this delivery runs under (bounds
	// applied), not the message's raw request.
	Options *common.MessageOptions `json:"options"`
}

type metaCtxKey struct{}

// WithMeta stamps meta onto the ctx a consumerFunc runs under. Exported only
// because each consumer package calls it; a caller stamping their own meta
// changes nothing outside their own ctx.
func WithMeta(ctx context.Context, meta MessageMeta) context.Context {
	return context.WithValue(ctx, metaCtxKey{}, meta)
}

// MetaFromContext retrieves MessageMeta from context within consumerFunc.
func MetaFromContext(ctx context.Context) (MessageMeta, bool) {
	meta, ok := ctx.Value(metaCtxKey{}).(MessageMeta)
	return meta, ok
}
