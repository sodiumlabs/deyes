package eth

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/golang/groupcache/lru"
	"github.com/sisu-network/lib/log"
	"github.com/sodiumlabs/deyes/chains"
	deyesethtypes "github.com/sodiumlabs/deyes/chains/eth/types"
	"github.com/sodiumlabs/deyes/config"
	"github.com/sodiumlabs/deyes/database"
	"github.com/sodiumlabs/deyes/types"

	chainstypes "github.com/sodiumlabs/deyes/chains/types"
)

const (
	minGasPrice      = 10_000_000_000
	TxTrackCacheSize = 1_000
)

type GasPriceGetter func(ctx context.Context) (*big.Int, error)

type BlockHeightExceededError struct {
	ChainHeight uint64
}

func NewBlockHeightExceededError(chainHeight uint64) error {
	return &BlockHeightExceededError{
		ChainHeight: chainHeight,
	}
}

func (e *BlockHeightExceededError) Error() string {
	return fmt.Sprintf("Our block height is higher than chain's height. Chain height = %d", e.ChainHeight)
}

type Watcher struct {
	cfg          config.Chain
	client       EthClient
	blockTime    int
	db           database.Database
	txsCh        chan *types.Txs
	txTrackCh    chan *chainstypes.TrackUpdate
	vault        string
	lock         *sync.RWMutex
	txTrackCache *lru.Cache
	gasCal       *gasCalculator

	// Block fetcher
	blockCh      chan *ethtypes.Block
	blockFetcher *defaultBlockFetcher

	// Receipt fetcher
	receiptFetcher    receiptFetcher
	receiptResponseCh chan *txReceiptResponse
}

func NewWatcher(db database.Database, cfg config.Chain, txsCh chan *types.Txs,
	txTrackCh chan *chainstypes.TrackUpdate, client EthClient) chains.Watcher {
	blockCh := make(chan *ethtypes.Block)
	receiptResponseCh := make(chan *txReceiptResponse)

	w := &Watcher{
		receiptResponseCh: receiptResponseCh,
		blockCh:           blockCh,
		blockFetcher:      newBlockFetcher(cfg, blockCh, client),
		receiptFetcher:    newReceiptFetcher(receiptResponseCh, client, cfg.Chain),
		db:                db,
		cfg:               cfg,
		txsCh:             txsCh,
		txTrackCh:         txTrackCh,
		blockTime:         cfg.BlockTime,
		client:            client,
		lock:              &sync.RWMutex{},
		txTrackCache:      lru.New(TxTrackCacheSize),
		gasCal:            newGasCalculator(cfg, client, GasPriceUpdateInterval),
	}

	return w
}

func (w *Watcher) init() {
	vaults, err := w.db.GetVaults(w.cfg.Chain)
	if err != nil {
		panic(err)
	}

	if len(vaults) > 0 {
		w.vault = vaults[0]
		log.Infof("Saved gateway in the db for chain %s is %s", w.cfg.Chain, w.vault)
	} else {
		log.Infof("Vault for chain %s is not set yet", w.cfg.Chain)
	}

	w.gasCal.Start()
}

func (w *Watcher) SetVault(addr string) {
	w.lock.Lock()
	defer w.lock.Unlock()

	log.Verbosef("Setting vault for chain %s with address %s", w.cfg.Chain, addr)
	err := w.db.SetVault(w.cfg.Chain, addr)
	if err == nil {
		w.vault = strings.ToLower(addr)
	} else {
		log.Error("Failed to save vault")
	}
}

func (w *Watcher) Start() {
	log.Info("Starting Watcher...")

	w.init()
	go w.scanBlocks()
}

func (w *Watcher) scanBlocks() {
	go w.blockFetcher.start()
	go w.receiptFetcher.start()

	go w.waitForBlock()
	go w.waitForReceipt()
}

// waitForBlock waits for new blocks from the block fetcher. It then filters interested txs and
// passes that to receipt fetcher to fetch receipt.
func (w *Watcher) waitForBlock() {
	for {
		block := <-w.blockCh

		// Pass this block to the receipt fetcher
		log.Info(w.cfg.Chain, " Block length = ", len(block.Transactions()))

		ret := w.processBlock(block)

		log.Info(w.cfg.Chain, " Filtered txs = ", len(ret.txs))

		if w.cfg.UseEip1559 {
			w.gasCal.AddNewBlock(block)
		}

		if len(ret.txs) > 0 {
			w.receiptResponseCh <- ret
		}

		// if len(ret.txs) > 0 {
		// 	w.receiptFetcher.fetchReceipts(block.Number().Int64(), ret.txs)
		// }
	}
}

// waitForReceipt waits for receipts returned by the fetcher.
func (w *Watcher) waitForReceipt() {
	for {
		response := <-w.receiptResponseCh
		txs := w.extractTxs(response)

		log.Verbose(w.cfg.Chain, ": txs sizes = ", len(txs.Arr))

		if len(txs.Arr) > 0 {
			// Send list of interested txs back to the listener.
			w.txsCh <- txs
		}

		// Save all txs into database for later references.
		w.db.SaveTxs(w.cfg.Chain, response.blockNumber, txs)
	}
}

// extractTxs takes response from the receipt fetcher and converts them into deyes transactions.
func (w *Watcher) extractTxs(response *txReceiptResponse) *types.Txs {
	arr := make([]*types.Tx, 0)
	for i, tx := range response.txs {
		receipt := response.receipts[i]

		bz, err := tx.MarshalBinary()
		if err != nil {
			log.Error("Cannot serialize ETH tx, err = ", err)
			continue
		}

		bzreceipt, err := receipt.MarshalBinary()

		if err != nil {
			log.Error("Cannot serialize ETH receipt, err = ", err)
			continue
		}

		if _, ok := w.txTrackCache.Get(tx.Hash().String()); ok {
			// Get Tx Receipt
			result := chainstypes.TrackResultConfirmed
			if receipt.Status == 0 {
				result = chainstypes.TrackResultFailure
			}

			// This is a transaction that we are tracking. Inform Sisu about this.
			w.txTrackCh <- &chainstypes.TrackUpdate{
				Chain:       w.cfg.Chain,
				Bytes:       bz,
				Hash:        tx.Hash().String(),
				BlockHeight: response.blockNumber,
				Result:      result,
			}

			continue
		}

		var to string
		if tx.To() == nil {
			to = ""
		} else {
			to = tx.To().String()
		}

		from, err := w.getFromAddress(w.cfg.Chain, tx)
		if err != nil {
			log.Errorf("cannot get from address for tx %s on chain %s, err = %v", tx.Hash().String(), w.cfg.Chain, err)
			continue
		}

		arr = append(arr, &types.Tx{
			Hash:              tx.Hash().String(),
			Serialized:        bz,
			ReceiptSerialized: bzreceipt,
			From:              from.Hex(),
			To:                to,
			VaultAddress:      w.vault,
			Success:           receipt.Status == 1,
		})
	}

	return &types.Txs{
		Chain:     w.cfg.Chain,
		ChainId:   w.cfg.ChainId,
		Block:     response.blockNumber,
		BlockHash: response.blockHash,
		Arr:       arr,
	}
}

func (w *Watcher) getSuggestedGasPrice() (*big.Int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RpcTimeOut)
	defer cancel()
	return w.client.SuggestGasPrice(ctx)
}

func (w *Watcher) processBlock(block *ethtypes.Block) *txReceiptResponse {
	ret := &txReceiptResponse{
		blockNumber: block.Number().Int64(),
		blockHash:   block.Hash().String(),
		txs:         make([]*ethtypes.Transaction, 0),
		receipts:    make([]*ethtypes.Receipt, 0),
	}

	if !block.Header().Bloom.Test([]byte(w.vault)) {
		log.Info("No vault address in bloom filter, skipping block", w.vault)
		return ret
	}

	txs := block.Transactions()

	for {
		if len(txs) == 0 {
			break
		}

		tx := txs[0]

		receipt, ok, err := w.acceptTx(tx)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				// If the transaction is not found, we can safely ignore it.
				// This is because we are using the block hash to query the transaction
				// receipt. If the block is not yet mined, the transaction will not be
				// found.
				txs = txs[1:]
				continue
			}
			log.Error("Failed to accept tx, err = ", err)
			// retry later
			time.Sleep(1 * time.Second)
			continue
		}

		if ok {
			ret.txs = append(ret.txs, tx)
			ret.receipts = append(ret.receipts, receipt)
		}

		txs = txs[1:]
	}

	return ret
}

func (w *Watcher) acceptTx(tx *ethtypes.Transaction) (*ethtypes.Receipt, bool, error) {
	if tx.To() == nil {
		return nil, false, nil
	}

	// 如果直接转给了 vault 地址，那么就不需要再去查询 receipt 了
	if strings.EqualFold(tx.To().String(), w.vault) {
		return nil, true, nil
	}

	// 支持直接通过ERC20, ERC721, ERC1155转账到vault地址
	receipt, err := w.client.TransactionReceipt(context.Background(), tx.Hash())
	if err != nil {
		return nil, false, err
	}

	for _, log := range receipt.Logs {
		// 如果是 vault 发出的交易
		if strings.EqualFold(log.Address.String(), w.vault) {
			return receipt, true, nil
		}
	}

	return nil, false, nil
}

func (w *Watcher) getFromAddress(chain string, tx *ethtypes.Transaction) (common.Address, error) {
	msg, err := tx.AsMessage(ethtypes.NewLondonSigner(tx.ChainId()), nil)
	if err != nil {
		return common.Address{}, err
	}

	return msg.From(), nil
}

func (w *Watcher) GetNonce(address string) (int64, error) {
	cAddr := common.HexToAddress(address)
	nonce, err := w.client.PendingNonceAt(context.Background(), cAddr)
	if err == nil {
		return int64(nonce), nil
	}

	return 0, fmt.Errorf("cannot get nonce of chain %s at %s", w.cfg.Chain, address)
}

func (w *Watcher) getGasPriceFromNode(ctx context.Context) (*big.Int, error) {
	gasPrice, err := w.getSuggestedGasPrice()
	if err != nil {
		log.Error("error when getting gas price", err)
		return big.NewInt(0), err
	}

	return gasPrice, nil
}

func (w *Watcher) TrackTx(txHash string) {
	log.Verbose("Tracking tx: ", txHash)
	w.txTrackCache.Add(txHash, true)
}

func (w *Watcher) getTransactionReceipt(txHash common.Hash) (*ethtypes.Receipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RpcTimeOut)
	defer cancel()
	receipt, err := w.client.TransactionReceipt(ctx, txHash)

	if err == nil {
		return receipt, nil
	}

	return nil, fmt.Errorf("Cannot find receipt for tx hash: %s", txHash.String())
}

func (w *Watcher) GetGasInfo() deyesethtypes.GasInfo {
	if w.cfg.UseEip1559 {
		return deyesethtypes.GasInfo{
			BaseFee: w.gasCal.GetBaseFee().Int64(),
			Tip:     w.gasCal.GetTip().Int64(),
		}
	} else {
		return deyesethtypes.GasInfo{
			GasPrice: w.gasCal.GetGasPrice().Int64(),
		}
	}
}
