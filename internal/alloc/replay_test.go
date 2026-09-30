package alloc

import (
	"strings"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/ir"
)

// TestExtremePressure keeps dozens of values live simultaneously with the
// minimum physical register count and requires correct results plus spills.
func TestExtremePressure(t *testing.T) {
	rng := newSeeded(1234)
	src := genLinear(40, 5, rng)
	p, err := ir.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{3, 4} {
		mp, stats, err := Compile(p, Config{NumPhysical: n, MemSize: 4096})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Spills < 30 {
			t.Fatalf("regs=%d: expected heavy spilling, got %d", n, stats.Spills)
		}
		diffCheck(t, src, []int{n})
		if mp == nil {
			t.Fatal("nil program")
		}
	}
}

// TestDeterministicReplay compiles the same input many times and requires
// byte-identical assembly regardless of iteration, exercising requirement 5.
func TestDeterministicReplay(t *testing.T) {
	srcs := []string{highPressure, crossCallProg, diamondProg, loopProg}
	for _, src := range srcs {
		p, err := ir.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		var golden string
		for i := 0; i < 5; i++ {
			mp, _, err := Compile(p, Config{NumPhysical: 3, MemSize: 512})
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				golden = mp.String()
			} else if mp.String() != golden {
				t.Fatalf("replay %d diverged for a source", i)
			}
		}
		if !strings.Contains(golden, "spill") && src != crossCallProg {
			t.Fatal("expected spills in replayed output")
		}
	}
}

// TestCallerSavedAcrossCalls requires live values to survive repeated calls.
func TestCallerSavedAcrossCalls(t *testing.T) {
	diffCheck(t, crossCallProg, []int{3, 4, 8})
}

// TestTooFewRegisters rejects machine models that cannot implement the
// three-address binary operations of the target ISA.
func TestTooFewRegisters(t *testing.T) {
	p, err := ir.Parse(highPressure)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(p, Config{NumPhysical: 2, MemSize: 64}); err == nil {
		t.Fatal("expected rejection for fewer than 3 physical registers")
	}
}
