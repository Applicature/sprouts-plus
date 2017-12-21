package sprouts

import (
	"bytes"
	"encoding/json"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

type coinAge struct {
	Time uint64 `json:"time"`
	Age  uint64 `json:"age"`
}

func (c *coinAge) bytes() []byte {
	b := new(big.Int).SetUint64(c.Age).Bytes()
	if len(b) < 20 {
		b = append(b, bytes.Repeat([]byte{0x00}, 20-len(b))...)
	}

	b = append(b, bytes.Repeat([]byte{0x00}, 32-20)...)
	copy(b[20:], new(big.Int).SetUint64(c.Time).Bytes())
	return b
}

func parseStake(stakeBytes []byte) (*coinAge, error) {
	if len(stakeBytes) != extraCoinAge {
		return nil, errWrongKernel
	}

	ca := new(coinAge)
	i := 0
	for ; i < len(stakeBytes); i++ {
		if stakeBytes[i] == 0 || i == 20 {
			break
		}
	}
	ca.Age = new(big.Int).SetBytes(stakeBytes[:i]).Uint64()

	for i = 20; i < len(stakeBytes); i++ {
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
		ca = &coinAge{0, uint64(time.Now().Unix())}
	} else {
		updatedAge := new(big.Int).SetUint64(ca.Age)
		updatedAge.Sub(updatedAge, stake)
		ca.Age = updatedAge.Uint64()
		ca.Time = uint64(time.Now().Unix())
	}
	ca.saveCoinAge(db, header.Coinbase)
}

type stake struct {
	Number    uint64      `json:"number"`
	Hash      common.Hash `json:"hash"`
	Timestamp uint64      `json:"timestamp"`
	Kernel    []byte      `json:"kernel"`
	Stake     uint64      `json:"stake"`
}

type mappedStakes map[common.Hash]stake

func (engine *PoS) getMappedStakes() (*mappedStakes, error) {
	// TODO implement caching as required
	return loadmappedStakes(engine.db)
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
		Stake:     ca.Age,
	}
	copy(stakeMap[header.Hash()].Kernel, header.Extra[len(header.Extra)-extraCoinAge-extraKernel:])

	go engine.saveMappedStakes(stakeMapP)
}

func (stakeMap mappedStakes) isDuplicate(stake *coinAge, kernel []byte) bool {
	eqArr := func(k1, k2 []byte) bool {
		if len(k1) != len(k2) {
			return false
		}
		for i := range k1 {
			if k1[i] != k2[i] {
				return false
			}
		}
		return true
	}
	for _, s := range stakeMap {
		if stake.Age == s.Stake && stake.Time == s.Timestamp && eqArr(kernel, s.Kernel) {
			return true
		}
	}
	return false
}

// func (s mappedStakes) validStake(header *types.Header, t *big.Int, kernel *big.Int) bool {
// 	hash := header.Hash()
// 	stake, ok := s[hash]
// 	if ok && stake.Timestamp == t.Uint64() {
// 		return true
// 	}
// 	return false
// }

func loadmappedStakes(db ethdb.Database) (*mappedStakes, error) {
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
