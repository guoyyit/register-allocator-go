package codegen

// Deterministic randomized equivalence probe over generated branch shapes:
// branch-dense regions, nested diamonds, loops whose condition value stays
// live in the body and after the exit, calls and stores interleaved with the
// branches, and an ever-growing live set that forces spills at edges. Fixed
// seeds make every failure exactly reproducible. For each generated program,
// several register counts and argument sets must produce the interpreter's
// return value and full memory image (which carries stored exit-live values).

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/interp"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

type fuzzGen struct {
	rng      *rand.Rand
	nextReg  int
	nextBlk  int
	nextAddr int
	body     string
	scope    []int // vregs definitely defined on all paths to here
}

func (g *fuzzGen) reg() int {
	r := g.nextReg
	g.nextReg++
	return r
}

func (g *fuzzGen) label(prefix string) string {
	g.nextBlk++
	return fmt.Sprintf("%s%d", prefix, g.nextBlk)
}

func (g *fuzzGen) pick() int {
	if len(g.scope) == 0 || g.rng.Intn(3) == 0 {
		v := g.reg()
		g.body += fmt.Sprintf("    v%d = %d\n", v, g.rng.Intn(21)-10)
		g.scope = append(g.scope, v)
		return v
	}
	return g.scope[g.rng.Intn(len(g.scope))]
}

var binops = []string{"add", "sub", "mul", "cmplt", "cmple", "cmpeq", "cmpne"}

func (g *fuzzGen) arith(depth int) {
	n := 1 + g.rng.Intn(3)
	for i := 0; i < n; i++ {
		a, b := g.pick(), g.pick()
		op := binops[g.rng.Intn(len(binops))]
		d := g.reg()
		g.body += fmt.Sprintf("    v%d = %s v%d, v%d\n", d, op, a, b)
		g.scope = append(g.scope, d)
	}
	// Occasionally call a callee, keeping values live across the call.
	if g.rng.Intn(4) == 0 && len(g.scope) >= 2 {
		a := g.scope[g.rng.Intn(len(g.scope))]
		b := g.scope[g.rng.Intn(len(g.scope))]
		d := g.reg()
		g.body += fmt.Sprintf("    v%d = callee(v%d, v%d)\n", d, a, b)
		g.scope = append(g.scope, d)
	}
	// Occasionally store a live value to memory.
	if g.rng.Intn(3) == 0 && len(g.scope) > 0 {
		v := g.scope[g.rng.Intn(len(g.scope))]
		base := g.reg()
		g.body += fmt.Sprintf("    v%d = %d\n", base, g.nextAddr)
		g.body += fmt.Sprintf("    store v%d, [v%d]\n", v, base)
		g.nextAddr++
	}
}

// region emits a branch region at the current position and returns the label
// of the merge block that subsequent code should jump into... actually it
// emits a full region ending by falling into the returned merge label.
func (g *fuzzGen) region(depth int) {
	cond := g.reg()
	a, b := g.pick(), g.pick()
	g.body += fmt.Sprintf("    v%d = cmplt v%d, v%d\n", cond, a, b)
	g.scope = append(g.scope, cond) // condition stays live into successors
	thenL := g.label("then")
	elseL := g.label("else")
	mergeL := g.label("merge")
	g.body += fmt.Sprintf("    br v%d, %s, %s\n", cond, thenL, elseL)

	saved := len(g.scope)
	g.body += "  " + thenL + ":\n"
	g.arith(depth)
	if depth > 0 && g.rng.Intn(2) == 0 {
		g.region(depth - 1)
	}
	g.body += fmt.Sprintf("    jmp %s\n", mergeL)

	g.scope = g.scope[:saved]
	g.body += "  " + elseL + ":\n"
	g.arith(depth)
	if depth > 0 && g.rng.Intn(2) == 0 {
		g.region(depth - 1)
	}
	g.body += fmt.Sprintf("    jmp %s\n", mergeL)

	g.scope = g.scope[:saved]
	g.body += "  " + mergeL + ":\n"
	// Use the condition again after the merge: it must still be current.
	d := g.reg()
	g.body += fmt.Sprintf("    v%d = add v%d, v%d\n", d, cond, cond)
	g.scope = append(g.scope, d)
}

func (g *fuzzGen) loop() {
	idx := g.reg()
	g.body += fmt.Sprintf("    v%d = 0\n", idx)
	g.scope = append(g.scope, idx)
	bound := g.reg()
	g.body += fmt.Sprintf("    v%d = %d\n", bound, 1+g.rng.Intn(5))
	head := g.label("loop")
	bodyL := g.label("body")
	exitL := g.label("exit")
	g.body += fmt.Sprintf("    jmp %s\n", head)
	g.body += "  " + head + ":\n"
	cond := g.reg()
	g.body += fmt.Sprintf("    v%d = cmplt v%d, v%d\n", cond, idx, bound)
	g.scope = append(g.scope, cond)
	g.body += fmt.Sprintf("    br v%d, %s, %s\n", cond, bodyL, exitL)
	bodyScope := len(g.scope)
	g.body += "  " + bodyL + ":\n"
	// Condition value reused inside the loop body.
	d := g.reg()
	g.body += fmt.Sprintf("    v%d = add v%d, v%d\n", d, cond, idx)
	g.scope = append(g.scope, d)
	g.arith(0)
	one := g.reg()
	g.body += fmt.Sprintf("    v%d = 1\n", one)
	ni := g.reg()
	g.body += fmt.Sprintf("    v%d = add v%d, v%d\n", ni, idx, one)
	g.body += fmt.Sprintf("    v%d = v%d\n", idx, ni)
	g.body += fmt.Sprintf("    jmp %s\n", head)
	// Body-local definitions do not dominate the exit block; drop them.
	g.scope = g.scope[:bodyScope]
	g.body += "  " + exitL + ":\n"
	// Condition value reused after loop exit.
	d2 := g.reg()
	g.body += fmt.Sprintf("    v%d = add v%d, v%d\n", d2, cond, cond)
	g.scope = append(g.scope, d2)
}

func TestRandomizedBranchEquivalence(t *testing.T) {
	const programs = 150
	for seed := int64(0); seed < programs; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := &fuzzGen{rng: rng, nextReg: 2}
		g.scope = append(g.scope, 0, 1) // params v0, v1
		src := "func callee(v0, v1):\n  entry:\n    v2 = add v0, v1\n    ret v2\n\n"
		src += "func main(v0, v1):\n  entry:\n"
		regions := 2 + rng.Intn(4)
		for i := 0; i < regions; i++ {
			if rng.Intn(3) == 0 {
				g.loop()
			} else {
				g.region(1 + rng.Intn(2))
			}
		}
		// Store a sample of live values so exit-live state is observable.
		for _, v := range g.scope {
			if g.nextAddr > 60 {
				break
			}
			if rng.Intn(2) == 0 {
				continue
			}
			base := g.reg()
			g.body += fmt.Sprintf("    v%d = %d\n", base, g.nextAddr)
			g.body += fmt.Sprintf("    store v%d, [v%d]\n", v, base)
			g.nextAddr++
		}
		// Accumulate everything live into the return value.
		acc := g.scope[len(g.scope)-1]
		for _, v := range g.scope[:len(g.scope)-1] {
			d := g.reg()
			g.body += fmt.Sprintf("    v%d = add v%d, v%d\n", d, acc, v)
			acc = d
		}
		g.body += fmt.Sprintf("    ret v%d\n", acc)
		src += g.body

		p, err := ir.Parse("fuzz.ir", src)
		if err != nil {
			t.Fatalf("seed %d parse: %v", seed, err)
		}
		if err := ir.Validate(p); err != nil {
			t.Fatalf("seed %d validate: %v\n%s", seed, err, src)
		}
		for _, args := range [][]int64{{3, 7}, {-4, 9}} {
			want, err := interp.NewMachine(256).Run(p, "main", args)
			if err != nil {
				t.Fatalf("seed %d ir: %v", seed, err)
			}
			for _, k := range []int{2, 3, 4, 8} {
				res, err := Generate(p, k)
				if err != nil {
					t.Fatalf("seed %d k=%d generate: %v", seed, k, err)
				}
				got, err := target.NewMachine(k, 256).Run(res.Program, "main", args)
				if err != nil {
					t.Fatalf("seed %d k=%d target: %v\n%s", seed, k, err, res.Program.String())
				}
				if got.RetVal != want.RetVal {
					t.Fatalf("seed %d k=%d args=%v ret=%d want %d\n%s", seed, k, args, got.RetVal, want.RetVal, src)
				}
				for i := range want.Memory {
					if got.Memory[i] != want.Memory[i] {
						t.Fatalf("seed %d k=%d args=%v mem[%d]=%d want %d\n%s", seed, k, args, i, got.Memory[i], want.Memory[i], src)
					}
				}
			}
		}
	}
}
