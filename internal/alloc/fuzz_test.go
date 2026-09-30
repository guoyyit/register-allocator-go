package alloc

import (
	"fmt"
	"math/rand"
	"testing"
)

func newSeeded(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

// genLinear builds a straight-line program that keeps nLive values alive
// while computing more, ending in a sum of all of them.
func genLinear(nLive, nExtra int, rng *rand.Rand) string {
	s := "func main\nentry:\n"
	for i := 1; i <= nLive; i++ {
		s += fmt.Sprintf("\t%%%d = const %d\n", i, rng.Intn(7)-3)
	}
	next := nLive + 1
	for i := 0; i < nExtra; i++ {
		a := 1 + rng.Intn(next-1)
		b := 1 + rng.Intn(next-1)
		ops := []string{"add", "sub", "mul"}
		s += fmt.Sprintf("\t%%%d = %s %%%d, %%%d\n", next, ops[rng.Intn(3)], a, b)
		next++
	}
	s += fmt.Sprintf("\tstore 0, %%%d\n", next-1)
	s += fmt.Sprintf("\tret %%%d\n", next-1)
	return s
}

func TestFuzzLinear(t *testing.T) {
	rng := rand.New(rand.NewSource(20260930))
	for iter := 0; iter < 60; iter++ {
		nLive := 1 + rng.Intn(20)
		nExtra := rng.Intn(20)
		src := genLinear(nLive, nExtra, rng)
		regs := 3 + rng.Intn(8)
		diffCheck(t, src, []int{regs})
	}
}

// genDiamond builds nested diamonds with constants live across branches.
func genDiamond(depth int, rng *rand.Rand) string {
	s := "func main\nentry:\n"
	s += "\t%1 = const 7\n\t%2 = const 3\n"
	next := 3
	var labels []string
	for d := 0; d < depth; d++ {
		tLabel := fmt.Sprintf("t%d", d)
		fLabel := fmt.Sprintf("f%d", d)
		jLabel := fmt.Sprintf("j%d", d)
		condReg := next
		s += fmt.Sprintf("\t%%%d = cmplt %%2, %%1\n", condReg)
		next++
		s += fmt.Sprintf("\tbrcond %%%d, %s, %s\n", condReg, tLabel, fLabel)
		s += tLabel + ":\n"
		s += fmt.Sprintf("\t%%%d = add %%1, %d\n", next, rng.Intn(5))
		next++
		s += "\tbr " + jLabel + "\n"
		s += fLabel + ":\n"
		s += fmt.Sprintf("\t%%%d = sub %%1, %d\n", next, rng.Intn(5))
		next++
		s += "\tbr " + jLabel + "\n"
		s += jLabel + ":\n"
		labels = append(labels, jLabel)
	}
	s += "\tstore 0, %1\n\tstore 1, %2\n"
	s += fmt.Sprintf("\t%%%d = load 0\n", next)
	next++
	s += fmt.Sprintf("\tret %%%d\n", next-1)
	return s
}

func TestFuzzDiamonds(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for d := 1; d <= 6; d++ {
		src := genDiamond(d, rng)
		diffCheck(t, src, []int{3, 4, 5})
	}
}
