package sprouts

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

type stake struct {
	Timestamp uint64 `json:"timestamp"`
	Kernel    uint64 `json:"kernel"`
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
