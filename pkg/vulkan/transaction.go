package vulkan

import (
	"context"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
)

// Tx is the surface handed to a ProducerFunc/TransactionFunc closure; the
// interface and its docs live with the datastore.
type Tx = producer.Tx

type TransactionFunc = producer.TransactionFunc

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits -- the way to publish to multiple targets atomically via ProduceInTx.
//
// It does not retry -- a transient blip or an ambiguous commit failure
// surfaces to you as-is. Wrap your own retry loop around it if you want one;
// only you know what's safe to rerun in your closure. Rerunning the whole
// closure is dedup-safe ONLY under caller-supplied IdempotencyKeys -- unset
// keys mint fresh per call, so a rerun double-publishes.
func InTransaction(ctx context.Context, ds *iDatastore.PostgresDatastore, transactionFunc TransactionFunc) error {
	return producer.InTransaction(ctx, ds, transactionFunc)
}
