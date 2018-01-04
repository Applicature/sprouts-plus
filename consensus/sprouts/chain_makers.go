// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package sprouts

import (
	"fmt"
	"math/big"

	"github.com/applicature/sprouts.next/params"
	"github.com/applicature/sprouts.next/common"
	"github.com/applicature/sprouts.next/consensus/misc"
	"github.com/applicature/sprouts.next/core"
	"github.com/applicature/sprouts.next/core/state"
	"github.com/applicature/sprouts.next/core/types"
	"github.com/applicature/sprouts.next/core/vm"
	"github.com/applicature/sprouts.next/ethdb"
)

// Modified copy of core/chain_makers.go to provide functionality needed
// for insertion into testing blockchain.
// Rationale for modification:
// Function core.GenerateChain is coupled with ethash consensus algorithm and
// doesn't have sufficient interfaces to modify header.Extra field.
// Come up with a more flexible approach in the future.

// So we can deterministically seed different blockchains
var (
	canonicalSeed = 1
	forkSeed      = 2
)

// BlockGen creates blocks for testing.
// See GenerateChain for a detailed explanation.
type BlockGen struct {
	i       int
	parent  *types.Block
	chain   []*types.Block
	header  *types.Header
	statedb *state.StateDB

	gasPool  *core.GasPool
	txs      []*types.Transaction
	receipts []*types.Receipt
	uncles   []*types.Header

	config *params.ChainConfig
}

// SetCoinbase sets the coinbase of the generated block.
// It can be called at most once.
func (b *BlockGen) SetCoinbase(addr common.Address) {
	if b.gasPool != nil {
		if len(b.txs) > 0 {
			panic("coinbase must be set before adding transactions")
		}
		panic("coinbase can only be set once")
	}
	b.header.Coinbase = addr
	b.gasPool = new(core.GasPool).AddGas(b.header.GasLimit)
}

// SetExtra sets the extra data field of the generated block.
func (b *BlockGen) SetExtra(data []byte) {
	b.header.Extra = data
}

// AddTx adds a transaction to the generated block. If no coinbase has
// been set, the block's coinbase is set to the zero address.
//
// AddTx panics if the transaction cannot be executed. In addition to
// the protocol-imposed limitations (gas limit, etc.), there are some
// further limitations on the content of transactions that can be
// added. Notably, contract code relying on the BLOCKHASH instruction
// will panic during execution.
func (b *BlockGen) AddTx(tx *types.Transaction) {
	if b.gasPool == nil {
		b.SetCoinbase(common.Address{})
	}
	b.statedb.Prepare(tx.Hash(), common.Hash{}, len(b.txs))
	receipt, _, err := core.ApplyTransaction(b.config, nil, &b.header.Coinbase, b.gasPool, b.statedb, b.header, tx, b.header.GasUsed, vm.Config{})
	if err != nil {
		panic(err)
	}
	b.txs = append(b.txs, tx)
	b.receipts = append(b.receipts, receipt)
}

// Number returns the block number of the block being generated.
func (b *BlockGen) Number() *big.Int {
	return new(big.Int).Set(b.header.Number)
}

// AddUncheckedReceipt forcefully adds a receipts to the block without a
// backing transaction.
//
// AddUncheckedReceipt will cause consensus failures when used during real
// chain processing. This is best used in conjunction with raw block insertion.
func (b *BlockGen) AddUncheckedReceipt(receipt *types.Receipt) {
	b.receipts = append(b.receipts, receipt)
}

// TxNonce returns the next valid transaction nonce for the
// account at addr. It panics if the account does not exist.
func (b *BlockGen) TxNonce(addr common.Address) uint64 {
	if !b.statedb.Exist(addr) {
		panic("account does not exist")
	}
	return b.statedb.GetNonce(addr)
}

// AddUncle adds an uncle header to the generated block.
func (b *BlockGen) AddUncle(h *types.Header) {
	b.uncles = append(b.uncles, h)
}

// PrevBlock returns a previously generated block by number. It panics if
// num is greater or equal to the number of the block being generated.
// For index -1, PrevBlock returns the parent block given to GenerateChain.
func (b *BlockGen) PrevBlock(index int) *types.Block {
	if index >= b.i {
		panic("block index out of range")
	}
	if index == -1 {
		return b.parent
	}
	return b.chain[index]
}

// OffsetTime modifies the time instance of a block, implicitly changing its
// associated difficulty. It's useful to test scenarios where forking is not
// tied to chain length directly.
func (b *BlockGen) OffsetTime(seconds int64) {
	b.header.Time.Add(b.header.Time, new(big.Int).SetInt64(seconds))
	if b.header.Time.Cmp(b.parent.Header().Time) <= 0 {
		panic("block time out of range")
	}
}

func (b *BlockGen) SetDifficulty(diff *big.Int) {
	b.header.Difficulty = new(big.Int).Set(diff)
}

func (b *BlockGen) Header() *types.Header {
	return b.header
}

// GenerateChain creates a chain of n blocks. The first block's
// parent will be the provided parent. db is used to store
// intermediate states and should contain the parent's state trie.
//
// The generator function is called with a new block generator for
// every block. Any transactions and uncles added to the generator
// become part of the block. If gen is nil, the blocks will be empty
// and their coinbase will be the zero address.
//
// Blocks created by GenerateChain do not contain valid proof of work
// values. Inserting them into BlockChain requires use of FakePow or
// a similar non-validating proof of work implementation.
func GenerateChain(sproutsConfig *params.SproutsConfig, config *params.ChainConfig, parent *types.Block, db ethdb.Database, n int, gen func(int, *BlockGen)) ([]*types.Block, []types.Receipts) {
	if config == nil {
		config = params.TestChainConfig
	}
	blocks, receipts := make(types.Blocks, n), make([]types.Receipts, n)
	genblock := func(i int, h *types.Header, statedb *state.StateDB) (*types.Block, types.Receipts) {
		b := &BlockGen{parent: parent, i: i, chain: blocks, header: h, statedb: statedb, config: config}
		// Mutate the state and block according to any hard-fork specs
		if daoBlock := config.DAOForkBlock; daoBlock != nil {
			limit := new(big.Int).Add(daoBlock, params.DAOForkExtraRange)
			if h.Number.Cmp(daoBlock) >= 0 && h.Number.Cmp(limit) < 0 {
				if config.DAOForkSupport {
					h.Extra = common.CopyBytes(params.DAOForkBlockExtra)
				}
			}
		}
		if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(h.Number) == 0 {
			misc.ApplyDAOHardFork(statedb)
		}
		// Execute any user modifications to the block and finalize it
		if gen != nil {
			gen(i, b)
		}

		accumulateRewards(sproutsConfig, h, statedb, nil, nil)
		root, err := statedb.CommitTo(db, config.IsEIP158(h.Number))
		if err != nil {
			panic(fmt.Sprintf("state write error: %v", err))
		}
		h.Root = root
		return types.NewBlock(h, b.txs, b.uncles, b.receipts), b.receipts
	}
	for i := 0; i < n; i++ {
		statedb, err := state.New(parent.Root(), state.NewDatabase(db))
		if err != nil {
			panic(err)
		}
		header := makeHeader(config, parent, statedb)
		block, receipt := genblock(i, header, statedb)
		blocks[i] = block
		receipts[i] = receipt
		parent = block
	}
	return blocks, receipts
}

func makeHeader(config *params.ChainConfig, parent *types.Block, state *state.StateDB) *types.Header {
	var time *big.Int
	if parent.Time() == nil {
		time = big.NewInt(10)
	} else {
		time = new(big.Int).Add(parent.Time(), big.NewInt(10)) // block time is fixed at 10 seconds
	}

	return &types.Header{
		Root:       state.IntermediateRoot(config.IsEIP158(big.NewInt(int64(parent.Number().Uint64() + 1)))),
		ParentHash: parent.Hash(),
		UncleHash:  types.CalcUncleHash(nil),
		Coinbase:   parent.Coinbase(),
		Difficulty: big.NewInt(1),
		GasLimit:   core.CalcGasLimit(parent),
		GasUsed:    new(big.Int),
		Number:     new(big.Int).Add(parent.Number(), common.Big1),
		Time:       time,
	}
}
