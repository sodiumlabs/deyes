package core

import (
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/sodiumlabs/deyes/chains"
	chainseth "github.com/sodiumlabs/deyes/chains/eth"

	"github.com/sisu-network/lib/log"
	chainstypes "github.com/sodiumlabs/deyes/chains/types"
	"github.com/sodiumlabs/deyes/client"
	"github.com/sodiumlabs/deyes/config"
	"github.com/sodiumlabs/deyes/database"
	"github.com/sodiumlabs/deyes/types"

	"github.com/sodiumlabs/deyes/core/oracle"
)

// This struct handles the logic in deyes.
// TODO: Make this processor to support multiple chains at the same time.
type Processor struct {
	db         database.Database
	txsCh      chan *types.Txs
	txTrackCh  chan *chainstypes.TrackUpdate
	chain      string
	blockTime  int
	sisuClient client.Client

	watchers    map[string]chains.Watcher
	dispatchers map[string]chains.Dispatcher
	cfg         config.Deyes
	tpm         oracle.TokenPriceManager

	sodiumReady atomic.Value
}

func NewProcessor(
	cfg *config.Deyes,
	db database.Database,
	sisuClient client.Client,
	tpm oracle.TokenPriceManager,
) *Processor {
	return &Processor{
		cfg:         *cfg,
		db:          db,
		watchers:    make(map[string]chains.Watcher),
		dispatchers: make(map[string]chains.Dispatcher),
		sisuClient:  sisuClient,
		tpm:         tpm,
	}
}

func (p *Processor) Start() {
	log.Info("Starting tx processor...")
	log.Info("tp.cfg.Chains = ", p.cfg.Chains)

	p.txsCh = make(chan *types.Txs, 1000)
	p.txTrackCh = make(chan *chainstypes.TrackUpdate, 1000)

	go p.listen()

	for chain, cfg := range p.cfg.Chains {
		log.Info("Supported chain and config: ", chain, cfg)

		var watcher chains.Watcher
		var dispatcher chains.Dispatcher
		client := chainseth.NewEthClients(cfg, p.cfg.UseExternalRpcsInfo)
		client.Start()
		watcher = chainseth.NewWatcher(p.db, cfg, p.txsCh, p.txTrackCh, client)
		dispatcher = chainseth.NewEhtDispatcher(chain, client)
		p.watchers[chain] = watcher
		go watcher.Start()
		p.dispatchers[chain] = dispatcher
		dispatcher.Start()
	}
}

func (p *Processor) listen() {
	// TODO: This is a temporary solution to handle the case when the sodium is not ready.
	p.sodiumReady.Store(true)
	for {
		select {
		case txs := <-p.txsCh:
			if p.sodiumReady.Load() == true {
				p.sisuClient.BroadcastTxs(txs)
			} else {
				log.Warnf("txs: sodium is not ready")
			}

		case txTrackUpdate := <-p.txTrackCh:
			log.Verbose("There is a tx to confirm with hash: ", txTrackUpdate.Hash)
			p.sisuClient.OnTxIncludedInBlock(txTrackUpdate)
		}
	}
}

func (tp *Processor) SetVault(addr string) {
	log.Infof("Setting vault, addr = %s", addr)
	for _, watcher := range tp.watchers {
		watcher.SetVault(addr)
	}
}

func (tp *Processor) DispatchTx(request *types.DispatchedTxRequest) {
	chain := request.Chain
	watcher := tp.GetWatcher(chain)
	if watcher == nil {
		log.Errorf("Cannot find watcher for chain %s", chain)
		tp.sisuClient.PostDeploymentResult(types.NewDispatchTxError(request, types.ErrGeneric))
		return
	}

	// If dispatching successful, add the tx to tracking.
	watcher.TrackTx(request.TxHash)

	dispatcher := tp.dispatchers[chain]
	var result *types.DispatchedTxResult
	if dispatcher == nil {
		log.Error(fmt.Errorf("Cannot find dispatcher for chain %s", chain))
		result = types.NewDispatchTxError(request, types.ErrGeneric)
	} else {
		log.Verbosef("Dispatching tx for chain %s with hash %s", request.Chain, request.TxHash)
		result = dispatcher.Dispatch(request)
	}

	log.Info("Posting result to sisu for chain ", chain, " tx hash = ", request.TxHash, " success = ", result.Success)
	tp.sisuClient.PostDeploymentResult(result)
}

func (tp *Processor) GetNonce(chain string, address string) (int64, error) {
	watcher := tp.GetWatcher(chain)
	if watcher == nil {
		return 0, fmt.Errorf("cannot find watcher for chain %s", chain)
	}
	return watcher.(*chainseth.Watcher).GetNonce(address)
}

func (tp *Processor) GetWatcher(chain string) chains.Watcher {
	return tp.watchers[chain]
}

func (p *Processor) SetSodiumReady(isReady bool) {
	p.sodiumReady.Store(isReady)
}

func (tp *Processor) GetTokenPrice(id string) (*big.Int, error) {
	return tp.tpm.GetPrice(id)
}
