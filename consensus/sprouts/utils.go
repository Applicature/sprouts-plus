package sprouts

import (
	"encoding/binary"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/rlp"
	lru "github.com/hashicorp/golang-lru"
)

var (
	big8   = big.NewInt(8)
	big100 = big.NewInt(100)
)

func computeDifficulty(chain consensus.ChainReader, number uint64) *big.Int {
	prevBlockTime := new(big.Int).Set(chain.GetHeaderByNumber(number - 1).Time)
	timeDelta := prevBlockTime.Sub(prevBlockTime, chain.GetHeaderByNumber(number-2).Time).Uint64()

	diff := new(big.Int).Set(chain.GetHeaderByNumber(number - 1).Difficulty)

	// 1 week / 10 min
	targetSpacing := uint64(10 * 60)
	nInt := uint64((7 * 24 * 60 * 60) / targetSpacing)

	diff.Mul(diff, new(big.Int).SetUint64((nInt-1)*targetSpacing+2*timeDelta))
	diff.Div(diff, new(big.Int).SetUint64((nInt+1)*targetSpacing))
	return diff
}

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

func (engine *PoS) calcCoinAge(chain consensus.ChainReader, block *types.Block, number uint64) *big.Int {
	age := engine.blockAge(block)

	// calc coin-seconds

	limit := new(big.Int).SetInt64(time.Now().Unix())
	limit.Sub(limit, coinAgePeriod)
	lastTime := block.Time()
	// don't go farther than the time limit
	for lastTime.Cmp(limit) == -1 {
		// traverse the blocks
		block = chain.GetBlock(block.ParentHash(), number-1)
		if block.Number().Uint64() == 0 {
			break
		}
		age.Add(age, engine.blockAge(block))
		age.Div(age, new(big.Int).SetUint64(centValue))
	}

	// calc coin-days

	age.Mul(age, new(big.Int).SetUint64(centValue))
	age.Div(age, new(big.Int).SetUint64(coinValue/(24*60*60)))
	return age
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

func (engine *PoS) computeKernel(stake *big.Int, header *types.Header) (hash *big.Int, timestamp *big.Int, err error) {
	hash = new(big.Int)
	timestamp = new(big.Int).SetInt64(0)
	err = errCantFindKernel

	now := uint64(time.Now().Unix())
	till := uint64(60)
	if now-header.Time.Uint64() < till {
		till = now - header.Time.Uint64()
	}

	for t := now; t >= now-till; t-- {
		target := new(big.Int).Set(header.Difficulty)
		target.Mul(target, stake)
		target.Mul(target, new(big.Int).SetUint64((now-header.Time.Uint64())/coinValue/(24*60*60)))

		rawHash := sha3.NewShake256()
		rawHash.Write(stakeModifier.Bytes())
		rawHash.Write(header.Time.Bytes())
		rawHash.Write([]byte(strconv.FormatUint(uint64(binary.Size(*header)), 10)))
		rawHash.Write([]byte(strconv.FormatUint(now, 10)))
		rawHash.Write([]byte(strconv.FormatUint(now-t, 10)))

		h := make([]byte, 32)
		rawHash.Read(h)
		hash.SetBytes(h)
		if hash.Cmp(target) == 1 {
			err = nil
			timestamp.SetUint64(t)
			return
		}
	}

	return
}

func (engine *PoS) checkKernelHash(chain consensus.ChainReader, header *types.Header) error {
	hash, timestamp, err := engine.computeKernel(
		engine.calcCoinAge(chain, chain.GetBlock(header.Hash(), header.Number.Uint64()), header.Number.Uint64()),
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
	kernel := header.Extra[len(header.Extra)-extraKernel:]
	for i := 0; i < extraKernel/2; i++ {
		if kernel[i] != hashAsBytes[i] {
			return errWrongKernel
		}
	}
	for i := extraKernel / 2; i < extraKernel; i++ {
		if kernel[i] != hashedTimestamp[i] {
			return errWrongKernel
		}
	}
	return nil
}

// 0.84 = netto reward
// 0.08 = charity (to a Sprouts+ address C)
// 0.08 = r&d (to a Sprouts+ address D)
func (engine *PoS) accumulateRewards(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	receipts []*types.Receipt) {
	// first transfer complete reward to miner
	reward := new(big.Int).Set(estimateBlockReward())
	state.AddBalance(header.Coinbase, reward)

	// now form and send transactions to charity and r&d
	charityReward := new(big.Int).Set(reward)
	rdReward := new(big.Int).Set(reward)
	charityReward.Div(charityReward, big100)
	charityReward.Mul(charityReward, big8)
	rdReward.Div(rdReward, big100)
	rdReward.Mul(rdReward, big8)

	// TODO confirm mechanism for rewards
	txs = append(txs, types.NewTransaction(0, engine.config.CharityAccount, charityReward, gasLimit, gasPrice, nil))
	txs = append(txs, types.NewTransaction(0, engine.config.RDAccount, rdReward, gasLimit, gasPrice, nil))
}

// total reward for the block
// 8% annual reward split in 365 daily rewards
func estimateBlockReward() *big.Int {
	reward := big.NewInt(0)
	// TODO should reward be dependent on anything?

	return reward
}

func calcReward(coinAge *big.Int) *big.Int {
	rewardCoinYear := uint64(centValue * 8)
	return new(big.Int).SetUint64(coinAge.Uint64() * 33 / (365*33 + 8) * rewardCoinYear)
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
		header.Extra[:len(header.Extra)-65], // Yes, this will panic if extra is too short
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
	signature := header.Extra[len(header.Extra)-extraSeal:]

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
