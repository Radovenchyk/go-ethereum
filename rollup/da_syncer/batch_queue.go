package da_syncer

import (
	"context"
	"fmt"
	"math"

	"github.com/scroll-tech/go-ethereum/core/rawdb"
	"github.com/scroll-tech/go-ethereum/ethdb"
	"github.com/scroll-tech/go-ethereum/rollup/da_syncer/da"
)

type BatchQueue struct {
	// batches is map from batchIndex to batch blocks
	batches                 map[uint64]da.Entry
	DAQueue                 *DAQueue
	db                      ethdb.Database
	lastFinalizedBatchIndex uint64
}

func NewBatchQueue(DAQueue *DAQueue, db ethdb.Database) *BatchQueue {
	return &BatchQueue{
		batches:                 make(map[uint64]da.Entry),
		DAQueue:                 DAQueue,
		db:                      db,
		lastFinalizedBatchIndex: 0,
	}
}

// NextBatch finds next finalized batch and returns data, that was committed in that batch
func (bq *BatchQueue) NextBatch(ctx context.Context) (da.Entry, error) {
	if batch, ok := bq.getFinalizedBatch(); ok {
		return batch, nil
	}
	for {
		daEntry, err := bq.DAQueue.NextDA(ctx)
		if err != nil {
			return nil, err
		}
		switch daEntry.Type() {
		case da.CommitBatchV0Type, da.CommitBatchV1Type, da.CommitBatchV2Type:
			bq.batches[daEntry.BatchIndex()] = daEntry
		case da.RevertBatchType:
			bq.deleteBatch(daEntry.BatchIndex())
		case da.FinalizeBatchType:
			if daEntry.BatchIndex() > bq.lastFinalizedBatchIndex {
				bq.lastFinalizedBatchIndex = daEntry.BatchIndex()
			}
			ret, ok := bq.getFinalizedBatch()
			if ok {
				return ret, nil
			} else {
				continue
			}
		default:
			return nil, fmt.Errorf("unexpected type of daEntry: %T", daEntry)
		}
	}
}

// getFinalizedBatch returns next finalized batch if there is available
func (bq *BatchQueue) getFinalizedBatch() (da.Entry, bool) {
	if len(bq.batches) == 0 {
		return nil, false
	}
	var minBatchIndex uint64 = math.MaxUint64
	for index := range bq.batches {
		if index < minBatchIndex {
			minBatchIndex = index
		}
	}
	if minBatchIndex <= bq.lastFinalizedBatchIndex {
		batch, ok := bq.batches[minBatchIndex]

		// this won't happen because wew just found minBatchIndex among map keys, but need to leave this check to pass CI
		if !ok {
			return nil, false
		}
		bq.deleteBatch(minBatchIndex)
		return batch, ok
	} else {
		return nil, false
	}
}

// deleteBatch deletes data committed in the batch from map, because this batch is reverted or finalized
// updates DASyncedL1BlockNumber
func (bq *BatchQueue) deleteBatch(batchIndex uint64) {
	batch, ok := bq.batches[batchIndex]
	if !ok {
		return
	}
	curBatchL1Height := batch.L1BlockNumber()
	delete(bq.batches, batchIndex)
	if len(bq.batches) == 0 {
		rawdb.WriteDASyncedL1BlockNumber(bq.db, curBatchL1Height)
		return
	}
	// we store here min height of currently loaded batches to be able to start syncing from the same place in case of restart
	var minBatchL1Height uint64 = math.MaxUint64
	for _, val := range bq.batches {
		if val.L1BlockNumber() < minBatchL1Height {
			minBatchL1Height = val.L1BlockNumber()
		}
	}

	rawdb.WriteDASyncedL1BlockNumber(bq.db, minBatchL1Height-1)
}
