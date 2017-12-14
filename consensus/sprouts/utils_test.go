package sprouts

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

// testerChainReader implements consensus.ChainReader to access the genesis
// block. All other methods and requests will panic.
type testerChainReader struct {
	db ethdb.Database
}

func (r *testerChainReader) Config() *params.ChainConfig                 { return params.AllCliqueProtocolChanges }
func (r *testerChainReader) CurrentHeader() *types.Header                { panic("not supported") }
func (r *testerChainReader) GetHeader(common.Hash, uint64) *types.Header { panic("not supported") }
func (r *testerChainReader) GetBlock(common.Hash, uint64) *types.Block   { panic("not supported") }
func (r *testerChainReader) GetHeaderByHash(common.Hash) *types.Header   { panic("not supported") }
func (r *testerChainReader) GetHeaderByNumber(number uint64) *types.Header {
	if number == 0 {
		return core.GetHeader(r.db, core.GetCanonicalHash(r.db, 0), 0)
	}
	panic("not supported")
}

func TestComputeKernel(t *testing.T) {
	// Kernel computations rely on time, so to ensure repeatability of tests
	// time must remain fixed.
	startDate := time.Date(2017, 12, 12, 13, 0, 0, 0, time.UTC)

	genesis := &core.Genesis{
		Timestamp: uint64(startDate.Unix()),
		ExtraData: make([]byte, extraDefault+extraSeal+extraKernel+extraCoinAge),
	}
	db, _ := ethdb.NewMemDatabase()
	genesis.Commit(db)

	// Header of the block we are trying to seal
	header := types.Header{
		Number:     big.NewInt(1),
		Time:       new(big.Int).SetUint64(uint64(startDate.Add(time.Second * 5).Unix())),
		Difficulty: new(big.Int).SetUint64(1),
	}

	cases := []struct {
		stake     *big.Int
		timestamp *big.Int
		err       error
	}{
		{new(big.Int).SetUint64(0), new(big.Int).SetUint64(0), errCantFindKernel},
		{new(big.Int).SetUint64(1), new(big.Int).SetUint64(36), nil},
		{new(big.Int).SetUint64(100), new(big.Int).SetUint64(6), nil},
		{new(big.Int).SetUint64(8), new(big.Int).SetUint64(6), nil},
	}

	engine := PoS{}
	chain := &testerChainReader{db: db}
	for _, test := range cases {
		h, ts, err := engine.computeKernel(chain, test.stake, &header)
		if err != test.err {
			t.Fatal(err)
		}
		if ts.Cmp(test.timestamp) != 0 {
			t.Fatalf("Incorrect kernel, hash: %d, timestamp: expected %d, got %d\n",
				h, test.timestamp, ts)
		}
	}
}

func TestComputeDifficulty(t *testing.T) {
	// Difficulty computations rely on time, so to ensure repeatability of tests
	// time must remain fixed.
	startDate := time.Date(2017, 12, 12, 13, 0, 0, 0, time.UTC)

	genesis := &core.Genesis{
		Timestamp: uint64(startDate.Unix()),
		ExtraData: make([]byte, extraDefault+extraSeal+extraKernel+extraCoinAge),
	}
	db, _ := ethdb.NewMemDatabase()
	genesis.Commit(db)

	// chain := &testerChainReader{db: db}
	// chain, _ := core.NewBlockChain(db, params.TestChainConfig, ethash.NewFaker(), vm.Config{})
	// defer chain.Stop()
}
