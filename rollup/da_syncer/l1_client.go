package da_syncer

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/scroll-tech/go-ethereum"
	"github.com/scroll-tech/go-ethereum/common"
	"github.com/scroll-tech/go-ethereum/core/types"
	"github.com/scroll-tech/go-ethereum/log"
	"github.com/scroll-tech/go-ethereum/params"
	"github.com/scroll-tech/go-ethereum/rpc"

	"github.com/scroll-tech/go-ethereum/rollup/sync_service"
)

// L1Client is a wrapper around EthClient that adds
// methods for conveniently collecting rollup events of ScrollChain contract.
type L1Client struct {
	client                        sync_service.EthClient
	scrollChainAddress            common.Address
	l1CommitBatchEventSignature   common.Hash
	l1RevertBatchEventSignature   common.Hash
	l1FinalizeBatchEventSignature common.Hash
}

// newL1Client initializes a new L1Client instance with the provided configuration.
// It checks for a valid scrollChainAddress and verifies the chain ID.
func newL1Client(ctx context.Context, genesisConfig *params.ChainConfig, l1Client sync_service.EthClient) (*L1Client, error) {

	scrollChainABI, err := scrollChainMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("failed to get scroll chain abi: %w", err)
	}

	scrollChainAddress := genesisConfig.Scroll.L1Config.ScrollChainAddress
	if scrollChainAddress == (common.Address{}) {
		return nil, errors.New("must pass non-zero scrollChainAddress to L1Client")
	}

	// sanity check: compare chain IDs
	got, err := l1Client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query L1 chain ID, err: %w", err)
	}
	if got.Cmp(big.NewInt(0).SetUint64(genesisConfig.Scroll.L1Config.L1ChainId)) != 0 {
		return nil, fmt.Errorf("unexpected chain ID, expected: %v, got: %v", genesisConfig.Scroll.L1Config.L1ChainId, got)
	}

	client := L1Client{
		client:                        l1Client,
		scrollChainAddress:            scrollChainAddress,
		l1CommitBatchEventSignature:   scrollChainABI.Events["CommitBatch"].ID,
		l1RevertBatchEventSignature:   scrollChainABI.Events["RevertBatch"].ID,
		l1FinalizeBatchEventSignature: scrollChainABI.Events["FinalizeBatch"].ID,
	}
	return &client, nil
}

// fetcRollupEventsInRange retrieves and parses commit/revert/finalize rollup events between block numbers: [from, to].
func (c *L1Client) fetchRollupEventsInRange(ctx context.Context, from, to uint64) ([]types.Log, error) {
	log.Trace("L1Client fetchRollupEventsInRange", "fromBlock", from, "toBlock", to)

	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(from)), // inclusive
		ToBlock:   big.NewInt(int64(to)),   // inclusive
		Addresses: []common.Address{
			c.scrollChainAddress,
		},
		Topics: make([][]common.Hash, 1),
	}
	query.Topics[0] = make([]common.Hash, 3)
	query.Topics[0][0] = c.l1CommitBatchEventSignature
	query.Topics[0][1] = c.l1RevertBatchEventSignature
	query.Topics[0][2] = c.l1FinalizeBatchEventSignature

	logs, err := c.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to filter logs, err: %w", err)
	}
	return logs, nil
}

// fetchTxData fetches tx data corresponding to given event log
func (c *L1Client) fetchTxData(ctx context.Context, vLog *types.Log) ([]byte, error) {
	tx, _, err := c.client.TransactionByHash(ctx, vLog.TxHash)
	if err != nil {
		log.Debug("failed to get transaction by hash, probably an unindexed transaction, fetching the whole block to get the transaction",
			"tx hash", vLog.TxHash.Hex(), "block number", vLog.BlockNumber, "block hash", vLog.BlockHash.Hex(), "err", err)
		block, err := c.client.BlockByHash(ctx, vLog.BlockHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get block by hash, block number: %v, block hash: %v, err: %w", vLog.BlockNumber, vLog.BlockHash.Hex(), err)
		}

		found := false
		for _, txInBlock := range block.Transactions() {
			if txInBlock.Hash() == vLog.TxHash {
				tx = txInBlock
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("transaction not found in the block, tx hash: %v, block number: %v, block hash: %v", vLog.TxHash.Hex(), vLog.BlockNumber, vLog.BlockHash.Hex())
		}
	}

	return tx.Data(), nil
}

// fetchTxBlobHash fetches tx blob hash corresponding to given event log
func (c *L1Client) fetchTxBlobHash(ctx context.Context, vLog *types.Log) (common.Hash, error) {
	tx, _, err := c.client.TransactionByHash(ctx, vLog.TxHash)
	if err != nil {
		log.Debug("failed to get transaction by hash, probably an unindexed transaction, fetching the whole block to get the transaction",
			"tx hash", vLog.TxHash.Hex(), "block number", vLog.BlockNumber, "block hash", vLog.BlockHash.Hex(), "err", err)
		block, err := c.client.BlockByHash(ctx, vLog.BlockHash)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to get block by hash, block number: %v, block hash: %v, err: %w", vLog.BlockNumber, vLog.BlockHash.Hex(), err)
		}

		found := false
		for _, txInBlock := range block.Transactions() {
			if txInBlock.Hash() == vLog.TxHash {
				tx = txInBlock
				found = true
				break
			}
		}
		if !found {
			return common.Hash{}, fmt.Errorf("transaction not found in the block, tx hash: %v, block number: %v, block hash: %v", vLog.TxHash.Hex(), vLog.BlockNumber, vLog.BlockHash.Hex())
		}
	}
	blobHashes := tx.BlobHashes()
	if len(blobHashes) == 0 {
		return common.Hash{}, fmt.Errorf("transaction does not contain any blobs, tx hash: %v", vLog.TxHash.Hex())
	}
	return blobHashes[0], nil
}

func (c *L1Client) getFinalizedBlockNumber(ctx context.Context) (*big.Int, error) {
	h, err := c.client.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return nil, err
	}
	return h.Number, nil
}