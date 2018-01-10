package sprouts

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"strconv"
	"time"

	"github.com/applicature/sprouts-plus/common"
	"github.com/applicature/sprouts-plus/consensus"
	"github.com/applicature/sprouts-plus/core/state"
	"github.com/applicature/sprouts-plus/core/types"
	"github.com/applicature/sprouts-plus/crypto"
	"github.com/applicature/sprouts-plus/crypto/sha3"
	"github.com/applicature/sprouts-plus/log"
	"github.com/applicature/sprouts-plus/params"
	"github.com/applicature/sprouts-plus/rlp"
	lru "github.com/hashicorp/golang-lru"
)

var (
	big0   = big.NewInt(0)
	big8   = big.NewInt(8)
	big16  = big.NewInt(16)
	big100 = big.NewInt(100)
)

var (
	stakeMaxAge         uint64 // stake age of full weight
	preAllocCoefficient = new(big.Int).Lsh(big.NewInt(1), 256-200)
)

func init() {
	d, _ := time.ParseDuration("2160h") // 90 days
	stakeMaxAge = uint64(d)
}

func computeDifficulty(chain consensus.ChainReader, number uint64) *big.Int {
	// return 100000 for the first three blocks
	if number < 3 {
		return big.NewInt(10)
	}

	diff := new(big.Int).Set(chain.GetHeaderByNumber(number - 1).Difficulty)

	// 1 week / 10 min
	targetSpacing := uint64(10 * 60)
	nInt := uint64((7 * 24 * 60 * 60) / targetSpacing)

	prevBlockTime := new(big.Int).Set(chain.GetHeaderByNumber(number - 1).Time)
	timeDelta := prevBlockTime.Sub(prevBlockTime, chain.GetHeaderByNumber(number-2).Time).Uint64()
	diff.Mul(diff, new(big.Int).SetUint64(((nInt-1)*targetSpacing + 2*timeDelta)))
	diff.Div(diff, new(big.Int).SetUint64((nInt+1)*targetSpacing))

	return diff
}

// stakeOfBlock checks if this block was mined by current signer and if so,
// returns the stake
func (engine *PoS) stakeOfBlock(block *types.Block) (*coinAge, bool) {
	if !engine.isItMe(block.Coinbase()) {
		return nil, false
	}
	stake, err := extractStake(block.Header())
	if err != nil {
		return nil, false
	}
	return stake, true
}

func (engine *PoS) blockAge(block *types.Block, timeDiff *big.Int) *big.Int {
	bAge := new(big.Int).Set(big0)
	caFromTx := new(big.Int)

	// coin-seconds:
	transactions := block.Transactions()
	for _, transaction := range transactions {
		if fromAddress, fromErr := From(transaction); fromErr == nil {
			// we count regular transaction to us only when they are old enough
			if engine.isItMe(fromAddress) && timeDiff.Cmp(engine.config.CoinAgeFermentation) == 1 {
				// coin age of transaction
				caFromTx.Set(transaction.Value())
				caFromTx.Mul(caFromTx, timeDiff)
				caFromTx.Div(caFromTx, new(big.Int).SetUint64(centValue))

				// this transaction should be taken from block age
				bAge.Sub(bAge, caFromTx)
				continue
			}

			// transactions from DistributionAccount should always be counted
			if equalAddresses(fromAddress, engine.config.DistributionAccount) {
				// coin age of transaction
				caFromTx.Set(transaction.Value())
				caFromTx.Mul(caFromTx, timeDiff)
				caFromTx.Mul(caFromTx, big.NewInt(100)) // experiment
				caFromTx.Div(caFromTx, new(big.Int).SetUint64(centValue))

				// this transaction should be added to block age
				bAge.Add(bAge, caFromTx)
				continue
			}
		}
		toAddress := transaction.To()

		if toAddress != nil && engine.isItMe(*toAddress) && timeDiff.Cmp(engine.config.CoinAgeFermentation) == 1 {
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

// only called by the sealer
func (engine *PoS) coinAge(chain consensus.ChainReader) *coinAge {
	lastCoinAge := &coinAge{0, 0}

	now := time.Now()

	accumulateCoinAge := func(fromTime, number uint64) {
		threeDaysAgo := uint64(now.AddDate(0, 0, -3).Unix())
		for {
			if number == 0 {
				// add premined value
				header := chain.GetHeaderByNumber(number)
				stateDB, err := state.New(header.Root, state.NewDatabase(engine.db))
				if err == nil {
					ba := big.NewInt(0).Set(stateDB.GetBalance(engine.signer))
					ba.Mul(ba, preAllocCoefficient)
					ba.Div(ba, new(big.Int).SetUint64(coinValue/(24*60*60)))
					lastCoinAge.Age += ba.Uint64()
				}
				return
			}

			header := chain.GetHeaderByNumber(number)
			if header == nil {
				return
			}
			t := new(big.Int).Set(header.Time).Uint64()
			if t > threeDaysAgo {
				if stake, isMyStake := engine.stakeOfBlock(chain.GetBlock(header.Hash(), number)); isMyStake {
					// can't use the staked amount yet
					lastCoinAge.Age -= stake.Age
				}
			}

			if t < fromTime {
				return
			}

			diffTime := uint64(now.Unix()) - t
			ba := engine.blockAge(chain.GetBlock(header.Hash(), number), new(big.Int).SetUint64(diffTime))
			lastCoinAge.Age += ba.Uint64()

			number--
		}
	}

	currentN := chain.CurrentHeader().Number.Uint64()
	if currentN > 0 {
		currentN--
	}
	accumulateCoinAge(uint64(now.Unix())-engine.config.CoinAgeLifetime.Uint64(), currentN)
	lastCoinAge.Time = uint64(time.Now().Unix())
	lastCoinAge.saveCoinAge(engine.db, engine.signer)

	return lastCoinAge
}

// not used at the moment
func (engine *PoS) getPremineCoinAge() *big.Int {
	genesis := engine.getGenesis()
	// count pre-allocated funds only for half a year
	if genesis.Timestamp < uint64(time.Now().AddDate(0, -6, 0).Unix()) {
		return big0
	}
	for address, genesisAccount := range genesis.Alloc {
		if len(address) > 0 && engine.isItMe(address) {
			return genesisAccount.Balance.Mul(genesisAccount.Balance, preAllocCoefficient)
		}
	}
	return big0
}

func extractStake(header *types.Header) (*coinAge, error) {
	stakeBytes := header.Extra[len(header.Extra)-extraSeal-extraCoinAge : len(header.Extra)-extraSeal]
	return parseStake(stakeBytes)
}

func extractKernel(header *types.Header) []byte {
	return header.Extra[len(header.Extra)-extraSeal-extraCoinAge-extraKernel : len(header.Extra)-extraSeal-extraCoinAge]
}

func (engine *PoS) isItMe(address common.Address) bool {
	return equalAddresses(address, engine.signer)
}

func equalAddresses(a, b common.Address) bool {
	return bytes.Equal(a.Bytes(), b.Bytes())
}

func (engine *PoS) computeKernel(prevBlock *types.Header, stake *big.Int, header *types.Header) (hash *big.Int, timestamp *big.Int, err error) {
	hash = new(big.Int)
	timestamp = new(big.Int).SetInt64(0)
	err = errCantFindKernel

	if header.Number.Uint64() < 1 || prevBlock == nil {
		return
	}

	// increase gradually target until kernel is found
	for t := 60; t >= 0; t-- {
		step := uint64(t)
		timeWeight := header.Time.Uint64() - step - prevBlock.Time.Uint64()
		if timeWeight > stakeMaxAge {
			timeWeight = stakeMaxAge
		}
		target := new(big.Int).Set(header.Difficulty)
		// target.Div(target, big.NewInt(100000))
		target.Mul(target, stake)
		target.Mul(target, new(big.Int).SetUint64(timeWeight))
		target.Div(target, new(big.Int).SetUint64(coinValue))
		target.Div(target, new(big.Int).SetUint64(24*60*60))

		rawHash := append(stakeModifier.Bytes(), prevBlock.Time.Bytes()...)
		rawHash = append(rawHash, []byte(strconv.FormatUint(uint64(binary.Size(*header)), 10))...)
		// rawHash = append(rawHash, []byte(strconv.FormatUint(prevBlock.Time.Uint64(), 10))...)
		rawHash = append(rawHash, []byte(strconv.FormatUint(header.Time.Uint64()-step, 10))...)
		h1 := sha256.New()
		h1.Write(rawHash)
		h2 := sha256.New()
		h2.Write(h1.Sum(nil))

		computedHash := new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(h2.Sum(nil))))
		log.Info("Attempt to find kernel", "hash", computedHash, "target", target, "diff", header.Difficulty, "stake", stake, "timeWeight", timeWeight)

		if computedHash.Cmp(target) == -1 {
			// kernel found
			err = nil
			hash.SetBytes(h2.Sum(nil))
			timestamp.SetUint64(step)
			return
		}
	}

	return
}

func (engine *PoS) checkKernelHash(prevBlock *types.Header, header *types.Header, stake *coinAge) error {
	if header.Number.Uint64() == 0 {
		// should never get here
		return errUnknownBlock
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
	kernel := extractKernel(header)

	// sometimes hash can take 31
	till := extraKernel / 2
	if len(hashAsBytes) < till {
		till = len(hashAsBytes)
	}

	if !bytes.Equal(kernel[:till], hashAsBytes) || !bytes.Equal(kernel[extraKernel/2:extraKernel], hashedTimestamp) {
		return errWrongKernel
	}

	return nil
}

// 0.84 = netto reward
// 0.08 = charity (to a Sprouts+ address C)
// 0.08 = r&d (to a Sprouts+ address D)
func accumulateRewards(config *params.SproutsConfig, header *types.Header, state *state.StateDB) {
	// first estimate complete reward
	reward := new(big.Int).Set(estimateBlockReward(header))

	// now form rewards to charity and r&d, which take 8% each
	bruttoReward := new(big.Int).Set(reward)
	bruttoReward.Mul(bruttoReward, big8)
	bruttoReward.Div(bruttoReward, big100)

	// minter's reward is the rest
	nettoReward := new(big.Int).Set(reward)
	nettoReward.Sub(nettoReward, bruttoReward)
	nettoReward.Sub(nettoReward, bruttoReward)

	// add rewards to balances
	state.AddBalance(header.Coinbase, nettoReward)
	state.AddBalance(config.RewardsCharityAccount, bruttoReward)
	state.AddBalance(config.RewardsRDAccount, bruttoReward)
}

// total reward for the block
// 8% annual reward split in 365 daily rewards
func estimateBlockReward(header *types.Header) *big.Int {
	stake, err := extractStake(header)
	if err != nil {
		log.Warn(err.Error())
		return big0
	}
	rewardCoinYear := uint64(centValue * 0.0212)
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
	if len(header.Extra) < extraDefault+extraKernel+extraCoinAge+extraSeal {
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
