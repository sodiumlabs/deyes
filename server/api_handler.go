package server

import (
	"math/big"

	"github.com/sisu-network/lib/log"
	chainseth "github.com/sodiumlabs/deyes/chains/eth"
	deyesethtypes "github.com/sodiumlabs/deyes/chains/eth/types"
	"github.com/sodiumlabs/deyes/core"
	"github.com/sodiumlabs/deyes/types"
)

type ApiHandler struct {
	processor *core.Processor
}

func NewApi(processor *core.Processor) *ApiHandler {
	return &ApiHandler{
		processor: processor,
	}
}

// Empty function for checking health only.
func (api *ApiHandler) Ping(source string) error {
	return nil
}

// Called by Sisu to indicate that the server is ready to receive messages.
func (api *ApiHandler) SetSodiumReady(isReady bool) {
	log.Infof("Setting SetSodiumReady, isReady = %s", isReady)
	api.processor.SetSodiumReady(isReady)
}

func (api *ApiHandler) SetVaultAddress(addr string) {
	api.processor.SetVault(addr)
}

func (api *ApiHandler) DispatchTx(request *types.DispatchedTxRequest) {
	api.processor.DispatchTx(request)
}

func (api *ApiHandler) GetTokenPrice(id string) (*big.Int, error) {
	return api.processor.GetTokenPrice(id)
}

///// ETH

func (api *ApiHandler) GetNonce(chain string, address string) (int64, error) {
	return api.processor.GetNonce(chain, address)
}

// This API only applies for ETH chains.
func (api *ApiHandler) GetGasInfo(chain string) *deyesethtypes.GasInfo {
	watcher := api.processor.GetWatcher(chain).(*chainseth.Watcher)
	gasInfo := watcher.GetGasInfo()
	return &gasInfo
}
