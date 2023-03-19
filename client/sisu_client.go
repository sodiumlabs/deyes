package client

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sisu-network/lib/log"
	chainstypes "github.com/sodiumlabs/deyes/chains/types"
	"github.com/sodiumlabs/deyes/types"
)

const (
	RETRY_TIME = 10 * time.Second
)

// A client that connects to Sisu server
type Client interface {
	TryDial()
	Ping(source string) error
	BroadcastTxs(txs *types.Txs) error
	PostDeploymentResult(result *types.DispatchedTxResult) error
	OnTxIncludedInBlock(txTrack *chainstypes.TrackUpdate) error
}

var (
	ErrSisuServerNotConnected = errors.New("sisu server is not connected")
)

type DefaultClient struct {
	client    *rpc.Client
	url       string
	connected bool
}

func NewClient(url string) Client {
	return &DefaultClient{
		url: url,
	}
}

func (c *DefaultClient) TryDial() {
	log.Info("Trying to dial Sisu server")

	for {
		log.Info("Dialing...", c.url)
		var err error
		c.client, err = rpc.DialContext(context.Background(), c.url)
		if err != nil {
			log.Error("Cannot connect to Sisu server err = ", err)
			time.Sleep(RETRY_TIME)
			continue
		}

		err = c.Ping("deyes")
		if err != nil {
			log.Error("Cannot ping sisu err = ", err)
			time.Sleep(RETRY_TIME)
			continue
		}

		c.connected = true
		break
	}

	log.Info("Sisu server is connected")
}

func (c *DefaultClient) Ping(source string) error {
	var result string
	err := c.client.CallContext(context.Background(), &result, "sodium_ping", source)
	return err
}

// TODO: Handle the case when broadcasting fails. In that case, we need to save the first Tx
// that we need to send to Sodium.
func (c *DefaultClient) BroadcastTxs(txs *types.Txs) error {
	log.Verbose("Broadcasting to sodium server...")

	txsJSON, err := json.Marshal(txs)

	if err != nil {
		log.Error("Cannot marshal txs to json, err = ", err)
		return err
	}

	var result string
	err = c.client.CallContext(context.Background(), &result, "sodium_postObservedTxs", txs)
	if err != nil {
		log.Error("Cannot broadcast tx to sodium, err = ", err, " result =", result, " txs =", string(txsJSON))
		return err
	}
	log.Verbose("Done broadcasting!")

	return nil
}

func (c *DefaultClient) PostDeploymentResult(result *types.DispatchedTxResult) error {
	log.Verbose("Sending Tx Deployment result back to Sisu...")

	var r string
	err := c.client.CallContext(context.Background(), &r, "tss_postDeploymentResult", result)
	if err != nil {
		log.Error("Cannot post tx deployment to sisu", "tx hash =", result.TxHash, "err = ", err)
		return err
	}

	return nil
}

func (c *DefaultClient) UpdateTokenPrices(prices []*types.TokenPrice) error {
	log.Verbose("Posting token prices back to Sisu...")

	var r string
	err := c.client.CallContext(context.Background(), &r, "tss_updateTokenPrices", prices)
	if err != nil {
		log.Error("Failed to update token prices, err = ", err)
		return err
	}

	return nil
}

func (c *DefaultClient) OnTxIncludedInBlock(txTrack *chainstypes.TrackUpdate) error {
	log.Verbose("Confirming transaction with Sisu...")

	var r string
	err := c.client.CallContext(context.Background(), &r, "tss_onTxIncludedInBlock", txTrack)
	if err != nil {
		log.Error("Failed confirm transaction, err = ", err)
		return err
	}

	return nil
}
