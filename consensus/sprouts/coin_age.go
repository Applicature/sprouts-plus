package sprouts

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
)

func (engine *PoS) blockAge(block *types.Block) *big.Int {
	bAge := big.NewInt(0)
	transactions := block.Transactions()
	for _, transaction := range transactions {
		toAddress := transaction.To()
		if toAddress == nil || !engine.isItMe(*toAddress) {
			continue
		}
		bAge.Add(bAge, transaction.Value())
	}
	return bAge
}

func (engine *PoS) calcCoinAge(chain consensus.ChainReader, block *types.Block, number uint64) int64 {
	age := engine.blockAge(block)

	// calc coin-seconds

	limit := new(big.Int).SetInt64(time.Now().Unix())
	limit.Sub(limit, coinAgePeriod)
	lastTime := block.Time()
	// don't go farther than the time limit
	for lastTime.Cmp(limit) == -1 {
		// traverse the blocks
		block = chain.GetBlock(block.ParentHash(), number-1)
		age.Add(age, engine.blockAge(block))
		age.Div(age, new(big.Int).SetUint64(centValue))
	}

	// calc coin-days

	age.Mul(age, new(big.Int).SetUint64(centValue))
	age.Div(age, new(big.Int).SetUint64(coinValue/(24*60*60)))
	return age.Int64()
}

// TODO is there an shortcut for this in Ethereum?
func (engine *PoS) isItMe(address common.Address) bool {
	for i := range address {
		if address[i] != engine.signer[i] {
			return false
		}
	}
	return true
}
