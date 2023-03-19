package chains

import (
	"github.com/sodiumlabs/deyes/types"
)

type Dispatcher interface {
	Start()
	Dispatch(request *types.DispatchedTxRequest) *types.DispatchedTxResult
}
