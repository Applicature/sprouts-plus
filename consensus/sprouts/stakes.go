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
	Timestamp uint64 `json:"timestamp"`
	Kernel    uint64 `json:"kernel"`
	Stake     uint64 `json:"stake"`
}

type stakeMap map[common.Hash]stake

func (s stakeMap) validStake(header *types.Header, t *big.Int, kernel *big.Int) bool {
	hash := header.Hash()
	stake, ok := s[hash]
	if ok && stake.Timestamp == t.Uint64() && stake.Kernel == kernel.Uint64() {
		return true
	}
	return false
}

func (s stakeMap) store(db ethdb.Database) error {
	// blob, err := json.Marshal(s)
	// if err != nil {
	// 	return err
	// }
	// common.BytesToHash(b)
	// return db.Put(append([]byte("clique-"), s.Hash[:]...), blob)
	return nil
}
