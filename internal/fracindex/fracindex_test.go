package fracindex

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestBetweenOrders(t *testing.T) {
	cases := [][2]string{{"", ""}, {"", "V"}, {"V", ""}, {"V", "W"}, {"a", "b"}, {"V", "V1"}, {"", "1"}, {"zz", ""}}
	for _, c := range cases {
		k, err := Between(c[0], c[1])
		if err != nil {
			t.Fatalf("Between(%q,%q): %v", c[0], c[1], err)
		}
		if (c[0] != "" && k <= c[0]) || (c[1] != "" && k >= c[1]) {
			t.Fatalf("Between(%q,%q) = %q, not between", c[0], c[1], k)
		}
	}
}

func TestRejectsBadNeighbours(t *testing.T) {
	if _, err := Between("b", "a"); err == nil {
		t.Fatal("accepted reversed neighbours")
	}
	if _, err := Between("a", "a"); err == nil {
		t.Fatal("accepted equal neighbours")
	}
}

// Random inserts keep a total order and keys stay reasonably short.
func TestRandomInsertsStaySorted(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	keys := []string{After("")}
	for range 2000 {
		i := r.IntN(len(keys) + 1)
		a, b := "", ""
		if i > 0 {
			a = keys[i-1]
		}
		if i < len(keys) {
			b = keys[i]
		}
		k, err := Between(a, b)
		if err != nil {
			t.Fatal(err)
		}
		keys = slices.Insert(keys, i, k)
	}
	if !slices.IsSorted(keys) {
		t.Fatal("keys not sorted")
	}
	for _, k := range keys {
		if len(k) > 64 {
			t.Fatalf("key too long: %d", len(k))
		}
	}
}
