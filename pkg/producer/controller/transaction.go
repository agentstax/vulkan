package controller

import (
	"context"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
)

// Tx is the surface handed to a ProducerFunc/TransactionFunc closure; the
// interface and its docs live with the datastore.
type Tx = datastore.Tx

type TransactionFunc func(ctx context.Context, tx Tx) error

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits -- the way to publish to multiple targets atomically via ProduceInTx.
//
// It does not retry -- a transient blip or an ambiguous commit failure
// surfaces to you as-is. Wrap your own retry loop around it if you want one;
// only you know what's safe to rerun in your closure. Rerunning the whole
// closure is dedup-safe ONLY under caller-supplied IdempotencyKeys -- unset
// keys mint fresh per call, so a rerun double-publishes.
func InTransaction(ctx context.Context, ds *coredatastore.PostgresDatastore, transactionFunc TransactionFunc) error {
	tx, err := ds.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := transactionFunc(ctx, datastore.NewTx(tx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
