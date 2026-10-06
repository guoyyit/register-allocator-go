package codegen

// Randomized differential tests: deterministic pseudo-random branchy
// programs are generated, then executed on the IR interpreter and on the
// generated target code across several register counts and argument sets.
// Every reachable path must produce identical final states (return value,
// full memory, and observed exit-live vregs). All randomness is seeded, so
// failures reproduce exactly.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// randProgram generates a deterministic pseudo-random IR program. The
// control-flow graph is a DAG (edges only to strictly later blocks or ret),
// so execution always terminates. All vregs are defined in the entry block
// (or are parameters), so every use anywhere is definitely defined; blocks
// freely redefine them to create joins, cross-block lifetimes and pressure.
//
// Branch conditions are frequently defined immediately before the branch
// and read again at the start of a successor block, which is the shape that
// stresses value hand-off across control-flow edges.
func randProgram(r *rand.Rand) string {
	const (
		nv     = 12 // vregs in main, all defined at entry
		nblk   = 9  // basic blocks in main
		memLim = 64 // memory addresses stay in [0, memLim)
	)
	var b strings.Builder
	// A deterministic callee exercising the calling convention under
	// branch-heavy callers.
	b.WriteString(`func callee(v0, v1):
  entry:
    v2 = mul v0, v1
    v3 = add v2, v1
    v4 = sub v3, v0
    ret v4
`)
	b.WriteString("func main(v1, v2):\n")

	// reg picks a random vreg; dataReg never picks v0, the memory base, so
	// addressing stays in bounds.
	reg := func() int { return r.Intn(nv) }
	dataReg := func() int { return 1 + r.Intn(nv-1) }

	binops := []string{"add", "sub", "mul", "cmplt", "cmple", "cmpeq", "cmpne"}

	// pending[i] holds extra instructions to emit at the start of block i,
	// planted by earlier blocks so that branch conditions are consumed
	// right after the edge.
	pending := map[int][]string{}

	// Entry block: define the memory base and every other vreg.
	b.WriteString("  entry:\n")
	b.WriteString("    v0 = 0\n")
	for v := 3; v < nv; v++ {
		fmt.Fprintf(&b, "    v%d = %d\n", v, r.Intn(21)-10)
	}
	b.WriteString("    jmp b1\n")

	for bi := 1; bi < nblk; bi++ {
		fmt.Fprintf(&b, "  b%d:\n", bi)
		for _, line := range pending[bi] {
			b.WriteString(line)
		}
		last := bi == nblk-1
		ninst := r.Intn(5)
		for i := 0; i < ninst; i++ {
			switch r.Intn(6) {
			case 0:
				fmt.Fprintf(&b, "    v%d = %d\n", dataReg(), r.Intn(41)-20)
			case 1:
				fmt.Fprintf(&b, "    v%d = %s v%d, v%d\n", dataReg(), binops[r.Intn(len(binops))], reg(), reg())
			case 2:
				fmt.Fprintf(&b, "    v%d = v%d\n", dataReg(), reg())
			case 3:
				fmt.Fprintf(&b, "    store v%d, [v0 + %d]\n", dataReg(), r.Intn(memLim))
			case 4:
				fmt.Fprintf(&b, "    v%d = [v0 + %d]\n", dataReg(), r.Intn(memLim))
			case 5:
				fmt.Fprintf(&b, "    v%d = callee(v%d, v%d)\n", dataReg(), reg(), reg())
			}
		}
		switch {
		case last:
			fmt.Fprintf(&b, "    ret v%d\n", reg())
		case r.Intn(4) == 0:
			fmt.Fprintf(&b, "    ret v%d\n", reg())
		case r.Intn(2) == 0:
			fmt.Fprintf(&b, "    jmp b%d\n", bi+1+r.Intn(nblk-bi-1))
		default:
			t1 := bi + 1 + r.Intn(nblk-bi-1)
			t2 := bi + 1 + r.Intn(nblk-bi-1)
			cond := reg()
			// Frequently define the condition right before the branch so
			// it is register-resident with a stale slot at the edge.
			if r.Intn(2) == 0 {
				fmt.Fprintf(&b, "    v%d = %s v%d, v%d\n", cond, binops[r.Intn(len(binops))], reg(), reg())
			}
			// Plant a use of the condition at the start of a successor.
			use := fmt.Sprintf("    v%d = %s v%d, v%d\n", dataReg(), binops[r.Intn(len(binops))], cond, reg())
			if r.Intn(2) == 0 {
				pending[t1] = append(pending[t1], use)
			} else {
				pending[t2] = append(pending[t2], use)
			}
			fmt.Fprintf(&b, "    br v%d, b%d, b%d\n", cond, t1, t2)
		}
	}
	return b.String()
}

func TestRandomizedBranchEquivalence(t *testing.T) {
	const programs = 40
	for seed := int64(0); seed < programs; seed++ {
		src := randProgram(rand.New(rand.NewSource(seed)))
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runEquivPaths(t, src, "main", allK, [][]int64{{3, -4}, {-7, 9}})
		})
	}
}
