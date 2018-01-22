package sprouts

import (
	"bytes"
	"encoding/json"
	"math/big"
	"time"

	"github.com/applicature/sprouts-plus/common"
	"github.com/applicature/sprouts-plus/core/state"
	"github.com/applicature/sprouts-plus/core/types"
	"github.com/applicature/sprouts-plus/ethdb"
)

type coinAge struct {
	Time uint64   `json:"time"`
	Age  *big.Int `json:"age"`
}

func (c *coinAge) bytes() []byte {
	encodedAge := c.Age.Bytes()

	encodedLength := big.NewInt(int64(len(encodedAge))).Bytes()

	encoded := append(encodedLength, encodedAge...)
	if len(encoded) < 20 {
		encoded = append(encoded, bytes.Repeat([]byte{0x00}, 20-len(encoded))...)
	}

	encoded = append(encoded, bytes.Repeat([]byte{0x00}, 32-20)...)
	copy(encoded[20:], new(big.Int).SetUint64(c.Time).Bytes())

	return encoded
}

func parseStake(stakeBytes []byte) (*coinAge, error) {
	if len(stakeBytes) != extraCoinAge {
		return nil, errInvalidStake
	}

	ca := new(coinAge)

	ageLength := new(big.Int).SetBytes(stakeBytes[:1]).Uint64()

	// We can safely assume that len(ageLength) == 1
	// Length can be up to 20 bytes, and that number can be encoded in one byte.
	ca.Age = new(big.Int).SetBytes(stakeBytes[1 : 1+ageLength])

	i := 20
	for ; i < len(stakeBytes); i++ {
		if stakeBytes[i] == 0 {
			break
		}
	}
	ca.Time = new(big.Int).SetBytes(stakeBytes[20:i]).Uint64()
	return ca, nil
}

func loadCoinAge(db ethdb.Database, hash common.Address) (*coinAge, error) {
	caData, err := db.Get(append([]byte("coinage"), hash[:]...))
	if err != nil {
		return nil, err
	}

	ca := new(coinAge)
	if err := json.Unmarshal(caData, ca); err != nil {
		return nil, err
	}
	return ca, nil
}

func (c *coinAge) saveCoinAge(db ethdb.Database, hash common.Address) error {
	blob, err := json.Marshal(c)
	if err != nil {
		return err
	}
	common.BytesToHash(blob)
	return db.Put(append([]byte("coinage"), hash[:]...), blob)
}

func reduceCoinAge(state *state.StateDB, db ethdb.Database, header *types.Header, stake *big.Int) {
	ca, err := loadCoinAge(db, header.Coinbase)
	if err != nil || stake == nil {
		ca = &coinAge{Age: new(big.Int).Set(big0), Time: uint64(time.Now().Unix())}
	} else {
		updatedAge := new(big.Int).Set(ca.Age)
		updatedAge.Sub(updatedAge, stake)
		ca.Age = updatedAge
		ca.Time = uint64(time.Now().Unix())
	}
	ca.saveCoinAge(db, header.Coinbase)
}

type stake struct {
	Number    uint64      `json:"number"`
	Hash      common.Hash `json:"hash"`
	Timestamp uint64      `json:"timestamp"`
	Kernel    []byte      `json:"kernel"`
	Stake     *big.Int    `json:"stake"`
}

type mappedStakes map[common.Hash]stake

func (engine *PoS) getMappedStakes() (*mappedStakes, error) {
	// TODO implement caching as required
	return loadMappedStakes(engine.db)
}

func (engine *PoS) saveMappedStakes(sm *mappedStakes) error {
	return sm.store(engine.db)
}

func (engine *PoS) addStake(header *types.Header, ca *coinAge) {
	stakeMapP, ok := engine.getMappedStakes()
	if ok != nil {
		return
	}
	stakeMap := *stakeMapP

	stakeMap[header.Hash()] = stake{
		Number:    header.Number.Uint64(),
		Hash:      header.Hash(),
		Timestamp: header.Time.Uint64(),
		Kernel:    make([]byte, extraKernel),
		Stake:     new(big.Int).Set(ca.Age),
	}
	copy(stakeMap[header.Hash()].Kernel, header.Extra[len(header.Extra)-extraCoinAge-extraKernel:])

	go engine.saveMappedStakes(stakeMapP)
}

func (stakeMap mappedStakes) isDuplicate(stake *coinAge, kernel []byte) bool {
	for _, s := range stakeMap {
		if stake.Age == s.Stake && stake.Time == s.Timestamp && bytes.Equal(kernel, s.Kernel) {
			return true
		}
	}
	return false
}

func loadMappedStakes(db ethdb.Database) (*mappedStakes, error) {
	blob, err := db.Get([]byte("mappedStakes"))
	if err != nil {
		return nil, err
	}
	smArr := make([]stake, 0)
	if err := json.Unmarshal(blob, smArr); err != nil {
		return nil, err
	}

	var stakeMap mappedStakes
	stakeMap = make(map[common.Hash]stake)

	for _, s := range smArr {
		stakeMap[s.Hash] = s
	}
	return &stakeMap, nil
}

func (stakeMap mappedStakes) store(db ethdb.Database) error {
	smArr := make([]stake, 0)
	for _, s := range stakeMap {
		smArr = append(smArr, s)
	}
	blob, err := json.Marshal(smArr)
	if err != nil {
		return err
	}
	common.BytesToHash(blob)
	return db.Put([]byte("mappedStakes"), blob)
}
