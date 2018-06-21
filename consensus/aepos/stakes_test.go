package aepos

import (
	"math/big"
	"testing"
)

func TestCoinAgeSerialization(t *testing.T) {
	age, _ := new(big.Int).SetString("120530826000000000041662500000", 10)
	value, _ := new(big.Int).SetString("1170000000000000003100", 10)
	max := big.NewInt(1)
	max.Lsh(stakeMaxValue, 224)

	cases := []coinAge{
		coinAge{Time: 0, Age: new(big.Int).SetUint64(0), Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(1), Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(100), Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(100123161), Value: new(big.Int).SetUint64(10)},
		coinAge{Time: 1257894000, Age: new(big.Int).SetUint64(0), Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 0, Age: new(big.Int).SetUint64(199999999999999999), Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 2257894001, Age: new(big.Int).SetUint64(390625000000), Value: new(big.Int).SetUint64(2310)},
		coinAge{Time: 1515155715, Age: new(big.Int).SetUint64(100000000000000), Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 0, Age: new(big.Int).SetUint64(100100000000000000), Value: new(big.Int).SetUint64(100100000000000000)},
		coinAge{Time: 1516631561, Age: stakeMaxAge, Value: new(big.Int).SetUint64(0)},
		coinAge{Time: 1516631561, Age: age, Value: value},
		coinAge{Time: 0, Age: stakeMaxAge, Value: stakeMaxValue},
	}

	for _, testcase := range cases {
		serialized := testcase.bytes()
		newCa, err := parseStake(serialized)
		if err != nil {
			t.Fatal("Can't parse serialized stake: ", err)
		}
		if testcase.Age.Cmp(newCa.Age) != 0 || testcase.Time != newCa.Time || testcase.Value.Cmp(newCa.Value) != 0 {
			t.Fatal("Coin age shouldn't have changed with serialization:", testcase, newCa)
		}
	}
}
