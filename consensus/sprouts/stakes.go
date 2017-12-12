package sprouts

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

type coinAge struct {
	Time uint64 `json:"time"`
	Age  uint64 `json:"age"`
}

func (c *coinAge) bytes() []byte {
	return []byte{}
}

func parseStake(stakeBytes []byte) (*coinAge, error) {
	ca := new(coinAge)
	if err := json.Unmarshal(stakeBytes, ca); err != nil {
		return nil, err
	}
	return ca, nil
}

func loadCoinAge(db ethdb.Database, hash common.Address) (*coinAge, error) {
	caData, err := db.Get(append([]byte("coinage"), hash[:]...))
	if err != nil {
		return nil, err
	}

	return parseStake(caData)
}

func (c *coinAge) saveCoinAge(db ethdb.Database, hash common.Address) error {
	blob, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return db.Put(append([]byte("coinage"), hash[:]...), blob)
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
