package sprouts

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/sha3"
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
		h, ts, err := engine.computeKernel(chain.GetHeaderByNumber(header.Number.Uint64()-1), test.stake, &header)
		if err != test.err {
			t.Fatal(err)
		}
		if ts.Cmp(test.timestamp) != 0 {
			t.Fatalf("Incorrect kernel, hash: %d, timestamp: expected %d, got %d\n",
				h, test.timestamp, ts)
		}
	}
}

func TestGeneration(t *testing.T) {

	db, _ := ethdb.NewMemDatabase()

	var (
		rewardsKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		rewardsAddr   = crypto.PubkeyToAddress(rewardsKey.PublicKey)
		sproutsConfig = params.SproutsConfig{RewardsAccount: rewardsAddr}
		engine        = New(&sproutsConfig, db)

		genesis = &core.Genesis{
			Config:     params.TestSproutsChainConfig,
			Timestamp:  uint64(startDate.Unix()),
			Difficulty: big.NewInt(1),
			ExtraData:  make([]byte, extraDefault+extraSeal+extraKernel+extraCoinAge),
			Alloc:      core.GenesisAlloc{rewardsAddr: {Balance: big.NewInt(10)}},
		}
		genesisBlock = genesis.MustCommit(db)

		// for tx
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		// this code generates a log
		code   = common.Hex2Bytes("60606040525b7f24ec1d3ff24c2f6ff210738839dbc339cd45a5294d85c79361016243157aae7b60405180905060405180910390a15b600a8060416000396000f360606040526008565b00")
		signer = types.NewEIP155Signer(genesis.Config.ChainId)
	)

	blockchain, err := core.NewBlockChain(db, genesis.Config, engine, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	blocks, _ :=
		GenerateChain(&sproutsConfig, params.TestChainConfig, genesisBlock, db, 1, func(i int, b *BlockGen) {
			// i starts from zero in GenerateChain so make sure that difficulty is non-zero
			b.SetDifficulty(big.NewInt(int64(i + 1)))

			// b.SetCoinbase(rewardsAddr)

			// get parent block
			parent := b.PrevBlock(-1)
			hash, timestamp, err := engine.computeKernel(parent.Header(), big.NewInt(100), b.Header())
			if err != nil {
				t.Fatal(err)
			}
			h := sha3.NewShake256()
			h.Write(timestamp.Bytes())
			hashedTimestamp := make([]byte, 32)
			h.Read(hashedTimestamp)

			coinAge := &coinAge{0, uint64(time.Now().Unix())}

			extra := make([]byte, extraDefault+extraSeal+extraKernel+extraCoinAge)
			copy(extra[len(extra)-extraCoinAge-extraKernel:], hash.Bytes())
			copy(extra[len(extra)-extraCoinAge-extraKernel/2:], hashedTimestamp)
			copy(extra[len(extra)-extraCoinAge:], coinAge.bytes())
			b.SetExtra(extra)

			if i == 1 {
				tx, err := types.SignTx(types.NewContractCreation(b.TxNonce(addr1), new(big.Int), big.NewInt(1000000), new(big.Int), code), signer, key1)
				if err != nil {
					t.Fatalf("failed to create tx: %v", err)
				}
				b.AddTx(tx)
			}
		})
	// Here VerifyHeaders and Finalize are being called internally
	if i, err := blockchain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert original chain[%d]: %v", i, err)
	}
	defer blockchain.Stop()
}

// Difficulty computations rely on time, so to ensure repeatability of tests
// time must remain fixed.
var startDate = time.Date(2017, 12, 12, 13, 0, 0, 0, time.UTC)
