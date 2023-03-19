package oracle

import (
	"math/big"

	"github.com/sodiumlabs/deyes/config"
)

type Provider interface {
	GetPrice(token config.Token) (*big.Int, error)
}
