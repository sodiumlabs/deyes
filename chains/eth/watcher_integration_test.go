package eth

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/sisu-network/lib/log"
	chainstypes "github.com/sodiumlabs/deyes/chains/types"
	"github.com/stretchr/testify/require"

	"github.com/sodiumlabs/deyes/config"
	"github.com/sodiumlabs/deyes/database"
	"github.com/sodiumlabs/deyes/types"
)

// Test
func TestIntegration_GetGasPrice(t *testing.T) {
	t.Skip()
	cfg := config.Deyes{
		Chains: map[string]config.Chain{
			"goerli-testnet": {
				Chain:      "goerli-testnet",
				UseEip1559: true,
				Rpcs:       []string{"https://polygon.llamarpc.com"},
				BlockTime:  5000,
				AdjustTime: 500,
			},
		},

		DBHost:   "127.0.0.1",
		DBSchema: "deyes",
		InMemory: true,
	}
	chainCfg := cfg.Chains["goerli-testnet"]

	db := database.NewDb(&cfg)
	err := db.Init()
	require.Nil(t, err)

	client := NewEthClients(chainCfg, false)
	w := NewWatcher(db, cfg.Chains["goerli-testnet"], make(chan *types.Txs),
		make(chan *chainstypes.TrackUpdate), client).(*Watcher)

	w.Start()

	go func() {
		for {
			gasInfo := w.GetGasInfo()
			log.Infof("gas price = %d, base fee = %d, tip = %d", gasInfo.GasPrice, gasInfo.BaseFee,
				gasInfo.Tip)

			time.Sleep(time.Millisecond * time.Duration(w.blockTime))
		}
	}()

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
}
