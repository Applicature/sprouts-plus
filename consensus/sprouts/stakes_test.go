package sprouts

import (
	"testing"
)

func TestCoinAgeSerialization(t *testing.T) {
	cases := []coinAge{
		coinAge{0, 0},
		coinAge{1257894000, 1},
		coinAge{1257894000, 100},
		coinAge{1257894000, 100123161},
		coinAge{1257894000, 0},
		coinAge{0, 199999999999999999},
		coinAge{2257894001, 390625000000},
		coinAge{1515155715, 100000000000000},
		coinAge{0, 100100000000000000},
	}

	for _, testcase := range cases {
		serialized := testcase.bytes()
		newCa, err := parseStake(serialized)
		if err != nil {
			t.Fatal("Can't parse serialized stake: ", err)
		}
		if testcase.Age != newCa.Age || testcase.Time != newCa.Time {
			t.Fatal("Coin age shouldn't have changed with serialization:", testcase, newCa)
		}
	}
}
