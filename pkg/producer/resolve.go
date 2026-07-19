package producer

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5/pgconn"
)

// batchFailureAction is what classify decided to do about one failed attempt.
type batchFailureAction int

const (
	failBatch      batchFailureAction = iota // no smarter option -- every request gets the error
	evictStatement                           // one statement deterministically rejected -- evict it, rerun the rest
	splitBatch                               // deterministic but unattributable -- isolate each request alone
)

// classifyBatchFailure maps every terminal attempt failure to exactly one
// action. Transients and the first missing partition never reach here --
// attemptBatch absorbs them.
func classifyBatchFailure(err error, failedIdx int) batchFailureAction {
	// checked FIRST -- these can carry a statement index that is a retry
	// artifact, NOT poison to evict
	if retry.IsRetryable(err) || errors.Is(err, context.DeadlineExceeded) {
		return failBatch
	}

	// a PgError WITH a statement index is the server rejecting that one
	// statement. Only the FIRST failure carries a trustworthy index --
	// statements after it never executed (the aborted txn ignores them), so
	// multiple poisons surface one per rerun
	var pgErr *pgconn.PgError
	if failedIdx >= 0 && errors.As(err, &pgErr) {
		return evictStatement
	}

	// EX: pgx failing to encode ONE payload fails the whole SendBatch
	// client-side -- deterministic, but no statement index to pin it on
	return splitBatch
}

// resolveBatch records an outcome on every operation in the batch: attempt,
// then apply the rulebook, until nothing is left unresolved.
func (b *batcher[Message]) resolveBatch(ctx context.Context, batch *batch[Message]) {
	for batch.size() > 0 {
		failedIdx, err := b.attemptBatch(ctx, batch)
		if err == nil {
			batch.recordAll(nil)
			return
		}

		switch classifyBatchFailure(err, failedIdx) {
		case evictStatement:
			batch.at(failedIdx).response.record(err)
			batch = batch.remove(failedIdx)
		case splitBatch:
			if batch.size() == 1 {
				batch.recordAll(err) // already alone -- the failure is genuinely its own
				return
			}
			for i := range batch.size() {
				b.resolveBatch(ctx, batch.single(i))
			}
			return
		case failBatch:
			batch.recordAll(err)
			return
		}
	}
}
