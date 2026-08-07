package message

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// MessageMeta is everything about a delivered message besides its payload,
// read inside consumerFunc via MetaFromContext.
type MessageMeta struct {
	Id             int64
	RoutingKey     string
	CompactionKey  string
	CompactionRank int64
	CreatedAt      time.Time

	// Options - the resolved MessageOptions this delivery runs under (bounds
	// applied), not the message's raw request.
	Options *common.MessageOptions
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
