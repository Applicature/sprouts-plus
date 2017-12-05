package sprouts

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	minBlockTime big.Int = big.Int{}
)

type PoS struct {
}

// signers set to the ones provided by the user.
func New(config *params.SproutsConfig, db ethdb.Database) *PoS {
	return &PoS{}
}

// Author retrieves the Ethereum address of the account that minted the given
// block, which may be different from the header's coinbase if a consensus
// engine is based on signatures.
func (engine *PoS) Author(header *types.Header) (common.Address, error) {
	// use ecrecover as in clique? why is it confined to that packge?
	return common.Address{}, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules of a
// given engine. Verifying the seal may be done optionally here, or explicitly
// via the VerifySeal method.
func (engine *PoS) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	// time, signature, parents, no uncles
	// header.UncleHash
	// nonce, difficulty, forks?
	return nil
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications (the order is that of
// the input slice).
func (engine *PoS) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	// more complex logic from ethash? <= computational complexity of header verification logic
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for i, header := range headers {
			// err := engine.VerifyHeader(chain, header, headers[:i])
			err := engine.VerifyHeader(chain, header, seals[i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of a given engine.
func (engine *PoS) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

// VerifySeal checks whether the crypto seal on a header is valid according to
// the consensus rules of the given engine.
func (engine *PoS) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	// score > 0, stakeholders
	return nil
}

// Prepare initializes the consensus fields of a block header according to the
// rules of a particular engine. The changes are executed inline.
func (engine *PoS) Prepare(chain consensus.ChainReader, header *types.Header) error {
	// ...
	return nil
}

// Finalize runs any post-transaction state modifications (e.g. block rewards)
// and assembles the final block.
// Note: The block header and state database might be updated to reflect any
// consensus rules that happen at finalization (e.g. block rewards).
func (engine *PoS) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	calcRewards(chain.Config(), state, header)

	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	// no uncles
	header.UncleHash = types.CalcUncleHash(nil)

	return types.NewBlock(header, txs, nil, receipts), nil
}

// Seal generates a new block for the given input block with the local miner's
// seal place on top.
func (engine *PoS) Seal(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	// main dish, sir!
	return nil, nil
}

// APIs returns the RPC APIs this consensus engine provides.
func (engine *PoS) APIs(chain consensus.ChainReader) []rpc.API {
	return nil
}

// 8% annual reward split in 365 daily rewards
// 0.84 = netto reward
// 0.08 = charity (to a Sprouts+ address C)
// 0.08 = r&d (to a Sprouts+ address D)
func calcRewards(chainConfig *params.ChainConfig, state *state.StateDB, header *types.Header) {

}
