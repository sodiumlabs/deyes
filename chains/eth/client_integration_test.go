package eth

import (
	"testing"

	"github.com/sisu-network/lib/log"
	"github.com/sodiumlabs/deyes/config"
	"github.com/stretchr/testify/require"
)

func TestIntegration_GetExtraRpcs(t *testing.T) {
	t.Skip()

	c := NewEthClients(
		config.Chain{
			Chain:      "goerli-testnet",
			UseEip1559: true,
			Rpcs:       make([]string, 0),
		},
		true,
	).(*defaultEthClient)
	rpcs, err := c.GetExtraRpcs()
	require.Nil(t, err)

	filterRpcs, _, _ := c.getRpcsHealthiness(rpcs)
	log.Verbose("filterRpcs = ", filterRpcs)
}
