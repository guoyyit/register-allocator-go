package alloc

import (
	"math/rand"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// TestNoSilentClobber exercises spill/reload behavior across a broad corpus.
// Differential execution catches any silent overwrite of a live value, while
// the slot check guarantees every reload reads a slot previously written by
// a spill, so values are never restored from uninitialized memory.
func TestNoSilentClobber(t *testing.T) {
	type prog struct {
		src  string
		regs []int
	}
	corpus := []prog{
		{highPressure, []int{3, 4, 8}},
		{branchProg, []int{3, 4}},
		{callProg, []int{3, 4}},
		{loopProg, []int{3, 4, 6}},
		{crossCallProg, []int{3, 4, 6}},
		{diamondProg, []int{3, 4}},
	}
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 40; i++ {
		corpus = append(corpus, prog{
			genLinear(1+rng.Intn(18), rng.Intn(15), rng),
			[]int{3, 4, 5 + rng.Intn(5)},
		})
	}
	for _, cp := range corpus {
		p, err := ir.Parse(cp.src)
		if err != nil {
			t.Fatal(err)
		}
		if err := ir.Validate(p); err != nil {
			t.Fatal(err)
		}
		for _, n := range cp.regs {
			mp, _, err := Compile(p, Config{NumPhysical: n, MemSize: 512})
			if err != nil {
				t.Fatal(err)
			}
			diffCheck(t, cp.src, []int{n})
			checkReloadsHaveSpills(t, mp)
		}
	}
}

// checkReloadsHaveSpills asserts that within every function, each spill slot
// that is reloaded was written by a preceding spill of the same slot.
func checkReloadsHaveSpills(t *testing.T, mp *target.MProgram) {
	t.Helper()
	for _, f := range mp.Funcs {
		spilled := map[int]bool{}
		for _, ins := range f.Instrs {
			switch ins.Op {
			case target.MOpSpill:
				spilled[ins.Slot] = true
			case target.MOpReload:
				if !spilled[ins.Slot] {
					t.Fatalf("%s: reload of slot %d before any spill", f.Name, ins.Slot)
				}
			}
		}
	}
}
