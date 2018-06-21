package aepos

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/applicature/sprouts-plus/common"
	"github.com/applicature/sprouts-plus/core"
	"github.com/applicature/sprouts-plus/core/state"
	"github.com/applicature/sprouts-plus/core/types"
	"github.com/applicature/sprouts-plus/core/vm"
	"github.com/applicature/sprouts-plus/crypto"
	"github.com/applicature/sprouts-plus/crypto/sha3"
	"github.com/applicature/sprouts-plus/ethdb"
	"github.com/applicature/sprouts-plus/params"
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

// Kernel and difficulty computations rely on time, so to ensure repeatability of tests
// time must remain fixed.
var (
	startDate     = time.Date(2017, 12, 12, 13, 0, 0, 0, time.UTC)
	rewardsKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	rewardsAddr   = crypto.PubkeyToAddress(rewardsKey.PublicKey)
	sproutsConfig = params.AeposConfig{
		RewardsCharityAccount: rewardsAddr,
		RewardsRDAccount:      rewardsAddr,
		CoinAgeLifetime:       big.NewInt(60 * 60 * 24 * 30 * 12),
		CoinAgeHoldingPeriod:  big.NewInt(60 * 60 * 24 * 1),
		CoinAgeFermentation:   big.NewInt(60 * 60 * 24 * 7),
		BlockPeriod:           10,
	}

	testKey, _ = crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
	testAddr   = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestComputeKernel(t *testing.T) {
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
		{new(big.Int).SetUint64(10000), new(big.Int).SetUint64(7), nil},
		{new(big.Int).SetUint64(1000000), new(big.Int).SetUint64(6), nil},
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

// shortut for generation key data structures
func initBlockchainStructures() (*ethdb.MemDatabase, *core.Genesis, *PoS) {
	db, _ := ethdb.NewMemDatabase()

	var (
		engine = New(&sproutsConfig, db)

		genesis = &core.Genesis{
			Config:     params.TestAuxiliumChainConfig,
			Timestamp:  uint64(startDate.Unix()),
			Difficulty: big0,
			ExtraData:  make([]byte, extraDefault+extraSeal+extraKernel+extraCoinAge),
			Alloc:      core.GenesisAlloc{rewardsAddr: {Balance: big.NewInt(10)}},
		}
	)

	engine.Authorize(rewardsAddr, nil)

	return db, genesis, engine
}

func TestGeneration(t *testing.T) {
	db, genesis, engine := initBlockchainStructures()
	genesisBlock := genesis.MustCommit(db)
	blockchain, err := core.NewBlockChain(db, genesis.Config, engine, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	// for tx
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		// this code generates a log
		code   = common.Hex2Bytes("60606040525b7f24ec1d3ff24c2f6ff210738839dbc339cd45a5294d85c79361016243157aae7b60405180905060405180910390a15b600a8060416000396000f360606040526008565b00")
		signer = types.NewEIP155Signer(genesis.Config.ChainId)
	)

	blocks, _ :=
		GenerateChain(&sproutsConfig, params.TestAuxiliumChainConfig, genesisBlock, db, 1000, func(i int, b *BlockGen) {
			// i starts from zero in GenerateChain so make sure that difficulty is non-zero
			b.SetDifficulty(big.NewInt(int64(i + 1)))

			b.SetCoinbase(rewardsAddr)

			// get parent block
			parent := b.PrevBlock(-1)
			// put large stake here to ensure that kernel is found
			hash, timestamp, err := engine.computeKernel(parent.Header(), big.NewInt(1000000), b.Header())
			if err != nil {
				t.Fatal(err)
			}
			h := sha3.NewShake256()
			h.Write(timestamp.Bytes())
			hashedTimestamp := make([]byte, 32)
			h.Read(hashedTimestamp)

			coinAge := &coinAge{Time: uint64(time.Now().Unix()), Age: new(big.Int).Set(big0)}

			extra := bytes.Repeat([]byte{0x00}, extraDefault+extraSeal+extraKernel+extraCoinAge)
			copy(extra[len(extra)-extraCoinAge-extraKernel:], hash.Bytes())
			copy(extra[len(extra)-extraCoinAge-extraKernel/2:], hashedTimestamp)
			copy(extra[len(extra)-extraCoinAge:], coinAge.bytes())
			b.SetExtra(extra)

			if i%2 == 1 {
				tx, err := types.SignTx(types.NewContractCreation(b.TxNonce(addr1), new(big.Int), big.NewInt(1000000), new(big.Int), code), signer, key1)
				if err != nil {
					t.Fatalf("failed to create tx: %v", err)
				}
				b.AddTx(tx)
			}
		})

	// Insert blocks one by one to ensure that chain is complete enough for all checks to execute
	for i := range blocks {
		// Here VerifyHeaders and Finalize are being called internally
		if _, err := blockchain.InsertChain(blocks[i : i+1]); err != nil {
			t.Fatalf("failed to insert original chain[%d]: %v", i, err)
		}
	}
	defer blockchain.Stop()
}

func TestComputeDifficulty(t *testing.T) {
	db, genesis, engine := initBlockchainStructures()
	genesisBlock := genesis.MustCommit(db)
	blockchain, err := core.NewBlockChain(db, genesis.Config, engine, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	// there is no reason to check further blocks as the time difference between blocks
	// during GenerateChain is const
	n := 4
	blocks, _ :=
		GenerateChain(&sproutsConfig, params.TestAuxiliumChainConfig, genesisBlock, db, n, func(i int, b *BlockGen) {
			// i starts from zero in GenerateChain so make sure that difficulty is non-zero
			b.SetDifficulty(common.Big1)

			b.SetCoinbase(rewardsAddr)

			// get parent block
			parent := b.PrevBlock(-1)
			hash, timestamp, err := engine.computeKernel(parent.Header(), big.NewInt(1000000), b.Header())
			if err != nil {
				t.Fatal(err)
			}
			h := sha3.NewShake256()
			h.Write(timestamp.Bytes())
			hashedTimestamp := make([]byte, 32)
			h.Read(hashedTimestamp)

			coinAge := &coinAge{Time: uint64(time.Now().Unix()), Age: new(big.Int).Set(big0)}

			extra := bytes.Repeat([]byte{0x00}, extraDefault+extraSeal+extraKernel+extraCoinAge)
			copy(extra[len(extra)-extraCoinAge-extraKernel:], hash.Bytes())
			copy(extra[len(extra)-extraCoinAge-extraKernel/2:], hashedTimestamp)
			copy(extra[len(extra)-extraCoinAge:], coinAge.bytes())
			b.SetExtra(extra)
		})

	// Insert blocks one by one to ensure that chain is complete enough for all checks to execute
	for i := range blocks {
		// Here VerifyHeaders and Finalize are being called internally
		if _, err := blockchain.InsertChain(blocks[i : i+1]); err != nil {
			t.Fatalf("failed to insert original chain[%d]: %v", i, err)
		}
	}
	defer blockchain.Stop()

	expectedDiff := []*big.Int{
		big.NewInt(100000),
		big.NewInt(100000),
		big.NewInt(998050),
		big.NewInt(998050),
	}

	for i := 1; i <= n; i++ {
		diff := computeDifficulty(blockchain, uint64(i))
		if diff.Cmp(expectedDiff[i-1]) != 0 {
			t.Fatalf("Incorrect difficulty, expected %d, got %d\n", expectedDiff[i-1].Uint64(), diff.Uint64())
		}
	}
}

func TestCoinAge(t *testing.T) {
	db, genesis, engine := initBlockchainStructures()

	// It must be more than a month for coin age to grow
	genesis.Timestamp = uint64(time.Now().AddDate(0, -2, 0).Unix())
	signer := types.NewEIP155Signer(genesis.Config.ChainId)
	genesis.Alloc[testAddr] = core.GenesisAccount{Balance: big.NewInt(1000000)}

	genesisBlock := genesis.MustCommit(db)
	blockchain, err := core.NewBlockChain(db, genesis.Config, engine, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	n := 4
	blocks, _ :=
		GenerateChain(&sproutsConfig, params.TestAuxiliumChainConfig, genesisBlock, db, n, func(i int, b *BlockGen) {
			b.SetDifficulty(big.NewInt(1))

			b.SetCoinbase(rewardsAddr)

			// get parent block
			parent := b.PrevBlock(-1)
			hash, timestamp, err := engine.computeKernel(parent.Header(), big.NewInt(1000000), b.Header())
			if err != nil {
				t.Fatal(err)
			}
			h := sha3.NewShake256()
			h.Write(timestamp.Bytes())
			hashedTimestamp := make([]byte, 32)
			h.Read(hashedTimestamp)

			coinAge := &coinAge{Age: new(big.Int).Set(big0), Time: uint64(time.Now().Unix())}

			extra := bytes.Repeat([]byte{0x00}, extraDefault+extraSeal+extraKernel+extraCoinAge)
			copy(extra[len(extra)-extraCoinAge-extraKernel:], hash.Bytes())
			copy(extra[len(extra)-extraCoinAge-extraKernel/2:], hashedTimestamp)
			copy(extra[len(extra)-extraCoinAge:], coinAge.bytes())
			b.SetExtra(extra)

			tx, err := types.SignTx(types.NewTransaction(b.TxNonce(testAddr), rewardsAddr, big.NewInt(10), big.NewInt(1000000), new(big.Int), nil), signer, testKey)
			if err != nil {
				t.Fatalf("failed to create tx: %v", err)
			}
			b.AddTx(tx)
		})

	// Insert blocks one by one to ensure that chain is complete enough for all checks to execute
	for i := range blocks {
		if _, err := blockchain.InsertChain(blocks[i : i+1]); err != nil {
			t.Fatalf("failed to insert original chain[%d]: %v", i, err)
		}
	}
	defer blockchain.Stop()

	coinage := engine.coinAge(blockchain)
	statedb, err := state.New(genesisBlock.Root(), state.NewDatabase(db))
	statedb.AddBalance(rewardsAddr, big.NewInt(10))

	coinageNew := engine.coinAge(blockchain)
	if coinage.Age.Cmp(big0) <= 0 || coinage.Time <= 0 || coinage.Age.Cmp(coinageNew.Age) != 0 || coinage.Time != coinageNew.Time {
		t.Fatal("incorrect coin age calculation, value shouldn't have changed:", coinage, coinageNew)
	}
}
