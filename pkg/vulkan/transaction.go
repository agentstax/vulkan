package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// Tx is the surface handed to a ProducerFunc/TransactionFunc closure; the
// interface and its docs live with the datastore.
type Tx = datastore.Tx

type TransactionFunc = datastore.TransactionFunc

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits -- the way to publish to multiple targets atomically via ProduceInTx.
//
// It does not retry -- a transient blip or an ambiguous commit failure
// surfaces to you as-is. Wrap your own retry loop around it if you want one;
// only you know what's safe to rerun in your closure. Rerunning the whole
// closure is dedup-safe ONLY under caller-supplied IdempotencyKeys -- unset
// keys mint fresh per call, so a rerun double-publishes.
func (c *Client) InTransaction(ctx context.Context, transactionFunc TransactionFunc) error {
	return datastore.InTransaction(ctx, c.ds, transactionFunc)
}
