package consumer

import (
	"context"
	"time"
)

// MessageMeta is everything about a delivered message besides its payload,
// read inside consumerFunc via MetaFromContext.
type MessageMeta struct {
	Id             int64
	RoutingKey     string
	CompactionKey  string
	CompactionRank int64
	CreatedAt      time.Time
}

type metaCtxKey struct{}

func withMeta(ctx context.Context, meta MessageMeta) context.Context {
	return context.WithValue(ctx, metaCtxKey{}, meta)
}

// MetaFromContext retrieves MessageMeta from context within consumerFunc.
func MetaFromContext(ctx context.Context) (MessageMeta, bool) {
	meta, ok := ctx.Value(metaCtxKey{}).(MessageMeta)
	return meta, ok
}

func (row MessageRow) toMessageMeta() MessageMeta {
	return MessageMeta{
		Id:             row.Id,
		RoutingKey:     row.RoutingKey,
		CompactionKey:  row.CompactionKey,
		CompactionRank: row.CompactionRank,
		CreatedAt:      row.CreatedAt,
	}
}

func (exception ClaimedException) toMessageMeta() MessageMeta {
	return MessageMeta{
		Id:             exception.MessageId,
		RoutingKey:     exception.RoutingKey,
		CompactionKey:  exception.CompactionKey,
		CompactionRank: exception.CompactionRank,
		CreatedAt:      exception.CreatedAt,
	}
}
