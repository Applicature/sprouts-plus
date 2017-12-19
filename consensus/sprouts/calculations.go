package sprouts

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	lru "github.com/hashicorp/golang-lru"
	ldberrors "github.com/syndtr/goleveldb/leveldb/errors"
)

var (
	big0   = big.NewInt(0)
	big8   = big.NewInt(8)
	big16  = big.NewInt(16)
	big100 = big.NewInt(100)
)

func computeDifficulty(chain consensus.ChainReader, number uint64) *big.Int {
	// return 100000 for the first three blocks
	if number < 3 {
		return big.NewInt(100000)
	}

	diff := new(big.Int).Set(chain.GetHeaderByNumber(number - 1).Difficulty)

	// 1 week / 10 min
	targetSpacing := uint64(10 * 60)
	nInt := uint64((7 * 24 * 60 * 60) / targetSpacing)

	prevBlockTime := new(big.Int).Set(chain.GetHeaderByNumber(number - 1).Time)
	timeDelta := prevBlockTime.Sub(prevBlockTime, chain.GetHeaderByNumber(number-2).Time).Uint64()
	diff.Mul(diff, new(big.Int).SetUint64(((nInt-1)*targetSpacing+2*timeDelta)*1000000))
	diff.Div(diff, new(big.Int).SetUint64((nInt+1)*targetSpacing))

	return diff
}

func (engine *PoS) blockAge(block *types.Block, timeDiff *big.Int) *big.Int {
	bAge := new(big.Int).Set(big0)
	caFromTx := new(big.Int)

	// coin-seconds:
	transactions := block.Transactions()
	for _, transaction := range transactions {
		if fromAddress, fromErr := From(transaction); fromErr == nil && engine.isItMe(fromAddress) {
			// coin age of transaction
			caFromTx.Set(transaction.Value())
			caFromTx.Mul(caFromTx, timeDiff)
			caFromTx.Div(caFromTx, new(big.Int).SetUint64(centValue))

			// this transaction should be taken from block age
			bAge.Sub(bAge, caFromTx)
			continue
		}
		toAddress := transaction.To()
		if toAddress != nil && engine.isItMe(*toAddress) {
			caFromTx.Set(transaction.Value())
			caFromTx.Mul(caFromTx, timeDiff)
			caFromTx.Div(caFromTx, new(big.Int).SetUint64(centValue))

			// this transaction should be added to block age
			bAge.Add(bAge, caFromTx)
		}
	}

	// coin-days:
	bAge.Mul(bAge, new(big.Int).SetUint64(centValue))
	bAge.Div(bAge, new(big.Int).SetUint64(coinValue/(24*60*60)))

	return bAge
}

func (engine *PoS) coinAge(chain consensus.ChainReader) *coinAge {
	lastCoinAge, err := loadCoinAge(engine.db, engine.signer)
	if err != nil && err.Error() != "not found" {
		return &coinAge{0, 0}
	}

	if err == ldberrors.ErrNotFound || lastCoinAge == nil {
		lastCoinAge = &coinAge{0, 0}
	}

	now := time.Now()

	accumulateCoinAge := func(fromTime, toTime, number uint64) {
		for {
			header := chain.GetHeaderByNumber(number)
			if header == nil {
				return
			}
			t := new(big.Int).Set(header.Time).Uint64()
			if t > fromTime && t < toTime {
				diffTime := toTime - t
				lastCoinAge.Age += engine.blockAge(chain.GetBlock(header.Hash(), number), new(big.Int).SetUint64(diffTime)).Uint64()
			}
			if t < fromTime {
				return
			}
			number -= 1
		}
	}

	// In case coin age is not saved in db or it is time to recalculate coin age
	if err.Error() != "not found" || lastCoinAge == nil || uint64(now.Unix())-lastCoinAge.Time > coinAgeRecalculate.Uint64() {
		// Let only transactions within a year and no younger than a month ago be valid for coin age computations.
		accumulateCoinAge(uint64(now.AddDate(-1, 0, 0).Unix()),
			uint64(now.AddDate(0, 0, -30).Unix()),
			chain.CurrentHeader().Number.Uint64())

		lastCoinAge.Time = uint64(time.Now().Unix())
		lastCoinAge.saveCoinAge(engine.db, engine.signer)
	}

	return lastCoinAge
}

func extractStake(header *types.Header) (*coinAge, error) {
	stakeBytes := header.Extra[len(header.Extra)-extraCoinAge:]
	return parseStake(stakeBytes)
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

func (engine *PoS) computeKernel(prevBlock *types.Header, stake *big.Int, header *types.Header) (hash *big.Int, timestamp *big.Int, err error) {
	hash = new(big.Int)
	timestamp = new(big.Int).SetInt64(0)
	err = errCantFindKernel

	if header.Number.Uint64() < 1 || prevBlock == nil {
		return
	}

	till := uint64(60)

	for t := uint64(0); t < till; t++ {
		timeWeight := header.Time.Uint64() - t - prevBlock.Time.Uint64()
		target := new(big.Int).Set(header.Difficulty)
		// target.Div(target, big.NewInt(100000))
		target.Mul(target, stake)
		target.Mul(target, new(big.Int).SetUint64(timeWeight))
		target.Div(target, new(big.Int).SetUint64(coinValue))
		target.Div(target, new(big.Int).SetUint64(24*60*60))

		rawHash := append(stakeModifier.Bytes(), prevBlock.Time.Bytes()...)
		rawHash = append(rawHash, []byte(strconv.FormatUint(uint64(binary.Size(*header)), 10))...)
		rawHash = append(rawHash, []byte(strconv.FormatUint(header.Time.Uint64()-t, 10))...)
		h1 := sha256.New()
		h1.Write(rawHash)
		h2 := sha256.New()
		h2.Write(h1.Sum(nil))

		if new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(h2.Sum(nil)))).Cmp(target) == -1 {
			// kernel found
			err = nil
			hash.SetBytes(h2.Sum(nil))
			timestamp.SetUint64(t)
			return
		}
	}

	return
}

func (engine *PoS) checkKernelHash(prevBlock *types.Header, header *types.Header) error {
	if header.Number.Uint64() == 0 {
		// should never get here
		return errUnknownBlock
	}

	stake, err := extractStake(header)
	if err != nil {
		return err
	}

	hash, timestamp, err := engine.computeKernel(
		prevBlock,
		new(big.Int).SetUint64(stake.Age),
		header)
	if err != nil {
		return err
	}

	h := sha3.NewShake256()
	h.Write(timestamp.Bytes())
	hashedTimestamp := make([]byte, 32)
	h.Read(hashedTimestamp)

	hashAsBytes := hash.Bytes()

	// compare kernel and timestamp
	kernel := header.Extra[len(header.Extra)-extraCoinAge-extraKernel:]

	// sometimes hash can take 31
	till := extraKernel / 2
	if len(hashAsBytes) < till {
		till = len(hashAsBytes)
	}
	for i := 0; i < till; i++ {
		if kernel[i] != hashAsBytes[i] {
			return errWrongKernel
		}
	}
	for i := extraKernel / 2; i < extraKernel; i++ {
		if kernel[i] != hashedTimestamp[i-extraKernel/2] {
			return errWrongKernel
		}
	}
	return nil
}

// 0.84 = netto reward
// 0.08 = charity (to a Sprouts+ address C)
// 0.08 = r&d (to a Sprouts+ address D)
func accumulateRewards(config *params.SproutsConfig, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	receipts []*types.Receipt) {
	// first estimate complete reward
	reward := new(big.Int).Set(estimateBlockReward(header))

	// now form rewards to charity and r&d, which take 16% combined
	bruttoReward := new(big.Int).Set(reward)
	bruttoReward.Div(bruttoReward, big100)
	bruttoReward.Mul(bruttoReward, big16)

	// minter's reward is the rest
	nettoReward := new(big.Int).Set(reward)
	nettoReward.Sub(nettoReward, bruttoReward)

	// add rewards to balances
	state.AddBalance(header.Coinbase, nettoReward)
	state.AddBalance(config.RewardsAccount, bruttoReward)
}

// total reward for the block
// 8% annual reward split in 365 daily rewards
func estimateBlockReward(header *types.Header) *big.Int {
	// TODO need to check error here?
	stake, _ := extractStake(header)
	rewardCoinYear := uint64(centValue * 8)
	return new(big.Int).SetUint64(stake.Age * 33 / (365*33 + 8) * rewardCoinYear)
}

// borrowing two PoA (clique) methods for signing blocks:

// sigHash returns the hash which is used as input for the proof-of-authority
// signing. It is the hash of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.
func sigHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewKeccak256()

	rlp.Encode(hasher, []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:len(header.Extra)-extraSeal], // Yes, this will panic if extra is too short
		header.MixDigest,
		header.Nonce,
	})
	hasher.Sum(hash[:0])
	return hash
}

// ecrecover extracts the Ethereum account address from a signed header.
func ecrecover(header *types.Header, sigcache *lru.ARCCache) (common.Address, error) {
	// If the signature's already cached, return that
	hash := header.Hash()
	if address, known := sigcache.Get(hash); known {
		return address.(common.Address), nil
	}
	// Retrieve the signature from the header extra-data
	if len(header.Extra) < extraSeal {
		return common.Address{}, errMissingSignature
	}
	signature := header.Extra[len(header.Extra)-extraSeal-extraKernel-extraCoinAge:]

	// Recover the public key and the Ethereum address
	pubkey, err := crypto.Ecrecover(sigHash(header).Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])

	sigcache.Add(hash, signer)
	return signer, nil
}

// borrowing Transaction function to derive "from" field from signature
func From(tx *types.Transaction) (common.Address, error) {
	v, _, _ := tx.RawSignatureValues()
	if v == nil {
		return common.Address{}, errors.New("invalid sender: nil V field")
	}
	if v.Sign() != 0 && tx.Protected() {
		var chainID *big.Int
		if v.BitLen() <= 64 {
			v := v.Uint64()
			if v == 27 || v == 28 {
				chainID = new(big.Int)
			}
			chainID = new(big.Int).SetUint64((v - 35) / 2)
		} else {
			v = new(big.Int).Sub(v, big.NewInt(35))
			chainID = v.Div(v, big.NewInt(2))
		}
		return types.NewEIP155Signer(chainID).Sender(tx)
	}
	signer := types.HomesteadSigner{}
	return signer.Sender(tx)
}
