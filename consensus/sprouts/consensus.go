package sprouts

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	lru "github.com/hashicorp/golang-lru"
)

const (
	inmemorySignatures = 4096 // Number of recent block signatures to keep in memory
	centValue          = 10000
	coinValue          = 1000000
)

var (
	coinAgePeriod *big.Int = new(big.Int).SetUint64(1000000) // how far down the chain to accumulate transaction values
	blockPeriod   uint64   = 15                              // min period between blocks
	minFee        int      = 1

	// Use these parameters for transactions with reward after minting.
	gasLimit *big.Int = new(big.Int).SetUint64(10)
	gasPrice *big.Int = new(big.Int).SetUint64(10)

	extraSeal = 65 // Fixed number of extra-data suffix bytes reserved for signer seal
)

// errors
var (
	errUnknownBlock = errors.New("unknown block")

	// errMissingSignature is returned if a block's extra-data section doesn't seem
	// to contain a 65 byte secp256k1 signature.
	errMissingSignature = errors.New("extra-data 65 byte suffix signature missing")

	errUnclesAreInvalid = errors.New("uncles are invalid")

	errInvalidSignature = errors.New("invalid signature")

	// errInvalidTimestamp is returned if the timestamp of a block is lower than
	// the previous block's timestamp + the minimum block period.
	errInvalidTimestamp = errors.New("invalid timestamp")

	errNotEnoughBalance = errors.New("not enough balance")

	errZeroTransactions = errors.New("zero sum transactions")
)

type PoS struct {
	config        *params.SproutsConfig
	signatures    *lru.ARCCache
	signer        common.Address
	signerFn      func(account accounts.Account, hash []byte) ([]byte, error)
	stakeModifier *big.Int
	lock          sync.RWMutex
}

// signers set to the ones provided by the user.
func New(config *params.SproutsConfig, db ethdb.Database) *PoS {
	signatures, _ := lru.NewARC(inmemorySignatures)
	return &PoS{
		config:        config,
		signatures:    signatures,
		stakeModifier: new(big.Int).SetInt64(0),
		lock:          sync.RWMutex{},
	}
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (engine *PoS) Authorize(signer common.Address, signFn func(account accounts.Account, hash []byte) ([]byte, error)) {
	engine.lock.Lock()
	defer engine.lock.Unlock()

	engine.signer = signer
	engine.signerFn = signFn
}

// Author retrieves the Ethereum address of the account that minted the given
// block, which may be different from the header's coinbase if a consensus
// engine is based on signatures.
func (engine *PoS) Author(header *types.Header) (common.Address, error) {
	return ecrecover(header, engine.signatures)
}

// VerifyHeader checks whether a header conforms to the consensus rules of a
// given engine. Verifying the seal may be done optionally here, or explicitly
// via the VerifySeal method.
func (engine *PoS) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	return engine.verifyHeader(chain, header, seal)
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

	return engine.checkKernelHash(header)
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
	engine.accumulateRewards(chain, header, state, txs, receipts)

	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	// no uncles
	header.UncleHash = types.CalcUncleHash(nil)

	return types.NewBlock(header, txs, nil, receipts), nil
}

// Seal generates a new block for the given input block with the local miner's
// seal place on top.
func (engine *PoS) Seal(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	header := block.Header()

	// Sealing the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return nil, errUnknownBlock
	}

	reward, err := engine.getKernel(block, stop)
	if err != nil {
		return nil, err
	}

	engine.lock.RLock()
	signer, signerFn := engine.signer, engine.signerFn
	engine.lock.RUnlock()

	if reward.Uint64() == 0 {
		return nil, errZeroTransactions
	}
	// TODO
	// if reward < balance {
	// return nil, errNotEnoughBalance
	// }

	age := engine.calcCoinAge(chain, block, number)
	// block coin age minimum 1 coin-day
	if age.Uint64() == 0 {
		age.SetUint64(1)
	}
	reward.Add(reward, calcReward(age))

	signature, err := signerFn(accounts.Account{Address: signer}, sigHash(header).Bytes())
	if err != nil {
		return nil, err
	}
	copy(header.Extra[len(header.Extra)-extraSeal:], signature)

	return block, nil
}

// APIs returns the RPC APIs this consensus engine provides.
func (engine *PoS) APIs(chain consensus.ChainReader) []rpc.API {
	return nil
}

func (engine *PoS) verifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	// who is this?
	if header.Number == nil {
		return consensus.ErrInvalidNumber
	}
	number := header.Number.Uint64()

	// no future blocks
	if header.Time.Cmp(big.NewInt(time.Now().Unix())) > 0 {
		return consensus.ErrFutureBlock
	}

	// no uncles
	if header.UncleHash != types.CalcUncleHash(nil) {
		return errUnclesAreInvalid
	}

	// signature check
	if len(header.Extra) < extraSeal {
		return errInvalidSignature
	}

	// for non-genesis block check parents
	if number != 0 {
		parent := chain.GetHeader(header.ParentHash, number-1)
		if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
			return consensus.ErrUnknownAncestor
		}

		if parent.Time.Uint64()+blockPeriod > header.Time.Uint64() {
			return errInvalidTimestamp
		}
	}

	if err := engine.checkKernelHash(header); err != nil {
		return err
	}

	// nonce, difficulty, forks?

	if seal {
		return engine.VerifySeal(chain, header)
	}
	return nil
}
