package sprouts

import (
	"math/big"
	"testing"
)

func TestCoinAgeSerialization(t *testing.T) {
	cases := []coinAge{
		coinAge{Time: 0, Age: new(big.Int).SetUint64(0)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(1)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(100)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(100123161)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(0)},
		coinAge{Time: 0, Age: new(big.Int).SetUint64(199999999999999999)},
		coinAge{Time: 2257894001, Age: new(big.Int).SetUint64(390625000000)},
		coinAge{Time: 1515155715, Age: new(big.Int).SetUint64(100000000000000)},
		coinAge{Time: 0, Age: new(big.Int).SetUint64(100100000000000000)},
		coinAge{Time: 1516631561, Age: stakeMaxAge},
	}

	for _, testcase := range cases {
		serialized := testcase.bytes()
		newCa, err := parseStake(serialized)
		if err != nil {
			t.Fatal("Can't parse serialized stake: ", err)
		}
		if testcase.Age.Cmp(newCa.Age) != 0 || testcase.Time != newCa.Time {
			t.Fatal("Coin age shouldn't have changed with serialization:", testcase, newCa)
		}
	}
}
