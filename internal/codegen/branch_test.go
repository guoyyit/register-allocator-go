package codegen

// Branch-shape regression tests. Every case runs the same program on the IR
// interpreter and on the generated target code across several physical
// register counts and several argument sets (so every reachable path is
// exercised), and requires identical final states: the return value, the
// full memory image, and every virtual register still live at the program
// exit. Exit-live values are made observable by instrumenting the entry
// function with stores that dump every definitely-defined vreg to a
// dedicated memory region right before each exit ret; the full-memory
// comparison then covers them.

import (
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/alloc"
	"github.com/guoyyit/register-allocator-go/internal/interp"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

const (
	branchMemSize = 4096
	// obsBase is the start of the memory region that exit-live observation
	// stores write to. Test programs must not use addresses at or above it.
	obsBase = 3072
)

// instrumentExitLive rewrites the entry function of p so that, just before
// every exit ret, every definitely-defined vreg is stored to
// mem[obsBase+vreg]. Both the interpreter and the target machine run the
// instrumented program, so comparing full memory compares the values still
// live at the program exit.
func instrumentExitLive(t *testing.T, p *ir.Program, entry string) {
	t.Helper()
	fi, ok := p.FuncIndex(entry)
	if !ok {
		t.Fatalf("entry function %q not found", entry)
	}
	f := p.Funcs[fi]
	n := alloc.VRegCount(f)
	obs := ir.Reg(n)

	// A fresh vreg holding the observation base address, defined in the
	// entry block so it is available on every path.
	entryBlk := f.Blocks[0]
	def := &ir.Inst{Op: ir.OpConst, Pos: entryBlk.Pos, Dst: obs, HasDst: true, Imm: obsBase}
	entryBlk.Inst = append([]*ir.Inst{def}, entryBlk.Inst...)

	in := definedSets(f, n+1)
	for bi, b := range f.Blocks {
		term := b.Inst[len(b.Inst)-1]
		if term.Op != ir.OpRet {
			continue
		}
		// Vregs definitely defined at this exit are exactly the values that
		// may still be live when the program terminates.
		defined := map[int]bool{}
		for v, ok := range in[bi] {
			if ok {
				defined[v] = true
			}
		}
		for _, ins := range b.Inst[:len(b.Inst)-1] {
			if d, ok := ins.Def(); ok {
				defined[int(d)] = true
			}
		}
		var stores []*ir.Inst
		for v := 0; v < n; v++ {
			if !defined[v] {
				continue
			}
			stores = append(stores, &ir.Inst{
				Op:      ir.OpStore,
				Pos:     term.Pos,
				Src1:    ir.Reg(v),
				HasSrc1: true,
				Dst:     obs,
				Imm:     int64(v),
			})
		}
		b.Inst = append(b.Inst[:len(b.Inst)-1], append(stores, term)...)
	}
}

// definedSets mirrors the must-be-defined data-flow analysis of ir.Validate
// and returns, for every block of f, the set of vregs defined on every path
// from the function entry to the block start.
func definedSets(f *ir.Func, n int) [][]bool {
	nb := len(f.Blocks)
	pred := make([][]int, nb)
	for bi, b := range f.Blocks {
		term := b.Inst[len(b.Inst)-1]
		switch term.Op {
		case ir.OpJmp:
			to, _ := f.BlockIndex(term.Label)
			pred[to] = append(pred[to], bi)
		case ir.OpBr:
			t1, _ := f.BlockIndex(term.Label)
			t2, _ := f.BlockIndex(term.Next)
			pred[t1] = append(pred[t1], bi)
			if t2 != t1 {
				pred[t2] = append(pred[t2], bi)
			}
		}
	}
	in := make([][]bool, nb)
	out := make([][]bool, nb)
	for bi := range in {
		in[bi] = make([]bool, n)
		out[bi] = make([]bool, n)
		for v := range out[bi] {
			out[bi][v] = true
		}
	}
	entry := make([]bool, n)
	for _, r := range f.Params {
		entry[int(r)] = true
	}
	changed := true
	for changed {
		changed = false
		for bi, b := range f.Blocks {
			var inset []bool
			switch {
			case bi == 0:
				inset = append([]bool(nil), entry...)
			case len(pred[bi]) == 0:
				inset = make([]bool, n)
				for v := range inset {
					inset[v] = true
				}
			default:
				inset = append([]bool(nil), out[pred[bi][0]]...)
				for _, pi := range pred[bi][1:] {
					for v := range inset {
						inset[v] = inset[v] && out[pi][v]
					}
				}
			}
			if !equalBoolSlice(in[bi], inset) {
				in[bi] = inset
				changed = true
			}
			cur := append([]bool(nil), inset...)
			for _, ins := range b.Inst {
				if d, ok := ins.Def(); ok {
					cur[int(d)] = true
				}
			}
			if !equalBoolSlice(out[bi], cur) {
				out[bi] = cur
				changed = true
			}
		}
	}
	return in
}

func equalBoolSlice(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runEquivPaths parses src, instruments the entry function for exit-live
// observation, and asserts that for every register count and every argument
// set the target machine's final state (return value, full memory, and
// thereby the observed exit-live vregs) matches the IR interpreter exactly.
func runEquivPaths(t *testing.T, src, entry string, ks []int, argsList [][]int64) {
	t.Helper()
	p, err := ir.Parse("case.ir", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ir.Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	instrumentExitLive(t, p, entry)
	if err := ir.Validate(p); err != nil {
		t.Fatalf("instrumented program invalid: %v", err)
	}
	for _, args := range argsList {
		want, err := interp.NewMachine(branchMemSize).Run(p, entry, args)
		if err != nil {
			t.Fatalf("ir run args=%v: %v", args, err)
		}
		for _, k := range ks {
			res, err := Generate(p, k)
			if err != nil {
				t.Fatalf("generate k=%d: %v", k, err)
			}
			got, err := target.NewMachine(k, branchMemSize).Run(res.Program, entry, args)
			if err != nil {
				t.Fatalf("target run k=%d args=%v: %v", k, args, err)
			}
			if got.RetVal != want.RetVal {
				t.Fatalf("k=%d args=%v: ret=%d want %d\n%s", k, args, got.RetVal, want.RetVal, res.Program.String())
			}
			for i := range want.Memory {
				if got.Memory[i] != want.Memory[i] {
					t.Fatalf("k=%d args=%v: mem[%d]=%d want %d\n%s", k, args, i, got.Memory[i], want.Memory[i], res.Program.String())
				}
			}
		}
	}
}

var allK = []int{2, 3, 4, 8}

// The branch condition is read again in both successor blocks.
func TestBranchConditionLiveOut(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 5
    v2 = 3
    v3 = add v1, v2
    v4 = add v3, v0
    br v4, then, else
  then:
    v5 = mul v4, v1
    ret v5
  else:
    v6 = sub v4, v2
    ret v6
`
	runEquivPaths(t, src, "main", allK, [][]int64{{2}, {-8}, {0}, {17}})
}

// The condition stays live into the successors while many other values
// compete for far fewer physical registers.
func TestBranchConditionLiveOutUnderPressure(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 1
    v2 = 2
    v3 = 3
    v4 = 4
    v5 = 5
    v6 = 6
    v7 = 7
    v8 = 8
    v9 = cmplt v0, v8
    br v9, then, else
  then:
    v10 = add v1, v2
    v11 = add v10, v3
    v12 = add v11, v4
    v13 = add v12, v9
    ret v13
  else:
    v20 = add v5, v6
    v21 = add v20, v7
    v22 = add v21, v8
    v23 = sub v22, v9
    ret v23
`
	runEquivPaths(t, src, "main", allK, [][]int64{{3}, {8}, {100}})
}

// Nested branches where the outer condition is still read inside the inner
// branches and is even reused as an inner branch condition.
func TestNestedBranches(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 2
    v2 = mul v0, v1
    v3 = 10
    v4 = cmplt v2, v3
    br v4, small, big
  small:
    v5 = add v2, v3
    v6 = 0
    v7 = cmplt v6, v0
    br v7, pos, nonpos
  pos:
    v8 = add v5, v4
    ret v8
  nonpos:
    v9 = sub v5, v4
    ret v9
  big:
    v10 = sub v2, v3
    br v4, weird, normal
  weird:
    ret v4
  normal:
    v11 = add v10, v4
    ret v11
`
	runEquivPaths(t, src, "main", allK, [][]int64{{3}, {-2}, {7}, {5}})
}

// Two predecessors define the same vreg differently; the join block reads it.
func TestMultiPredecessorJoin(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 0
    v2 = cmplt v1, v0
    br v2, pos, neg
  pos:
    v3 = 100
    jmp join
  neg:
    v3 = 200
    jmp join
  join:
    v4 = add v3, v0
    v5 = mul v4, v3
    ret v5
`
	runEquivPaths(t, src, "main", allK, [][]int64{{4}, {-1}, {0}})
}

// A chain of branch blocks whose conditions all stay live into the leaves.
func TestBranchChain(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 1
    v2 = 2
    v3 = 3
    v4 = cmplt v0, v1
    br v4, l1, l2
  l1:
    v5 = cmplt v0, v2
    br v5, a, b
  l2:
    v6 = cmplt v0, v3
    br v6, c, d
  a:
    v7 = add v4, v5
    ret v7
  b:
    v8 = sub v5, v4
    ret v8
  c:
    v9 = add v4, v6
    ret v9
  d:
    v10 = sub v6, v4
    ret v10
`
	runEquivPaths(t, src, "main", allK, [][]int64{{0}, {1}, {2}, {5}})
}

// The condition value crosses a function call inside a successor block.
func TestBranchConditionAcrossCall(t *testing.T) {
	src := `
func bump(v0):
  entry:
    v1 = 1
    v2 = add v0, v1
    ret v2
func main(v0):
  entry:
    v1 = 4
    v2 = cmplt v1, v0
    br v2, big, small
  big:
    v3 = bump(v0)
    v4 = add v3, v2
    ret v4
  small:
    v5 = bump(v2)
    ret v5
`
	runEquivPaths(t, src, "main", allK, [][]int64{{9}, {1}, {4}})
}

// store vX, [vX]: the data and the address base are the same vreg, so the
// base must not be reloaded from a stale slot while the value sits in a
// register reserved by the data operand.
func TestStoreSameVReg(t *testing.T) {
	src := `
func main():
  entry:
    v1 = 5
    v2 = 100
    store v2, [v1]
    store v1, [v1]
    v3 = [v1]
    v4 = add v3, v1
    ret v4
`
	runEquivPaths(t, src, "main", allK, [][]int64{nil})
}

// A loop back-edge to the entry block of a function with parameters: the
// parameter homing must not run again on the back-edge.
func TestEntryBackEdge(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 1
    v0 = sub v0, v1
    v2 = 0
    v3 = cmplt v2, v0
    br v3, entry, done
  done:
    ret v0
`
	runEquivPaths(t, src, "main", allK, [][]int64{{3}, {0}, {5}, {1}})
}

// Same back-edge shape in a function without parameters.
func TestEntryBackEdgeNoParams(t *testing.T) {
	src := `
func main():
  entry:
    v0 = 4
    jmp loop
  loop:
    v1 = 1
    v0 = sub v0, v1
    v2 = 0
    v3 = cmplt v2, v0
    br v3, loop, done
  done:
    ret v0
`
	runEquivPaths(t, src, "main", allK, [][]int64{nil})
}

// Several parameters stay live across an entry back-edge together with
// values defined in the entry block itself.
func TestEntryBackEdgeMultiParam(t *testing.T) {
	src := `
func main(v0, v1):
  entry:
    v2 = add v0, v1
    v0 = sub v0, v1
    v3 = 0
    v4 = cmplt v3, v0
    br v4, entry, done
  done:
    v5 = add v2, v1
    ret v5
`
	runEquivPaths(t, src, "main", allK, [][]int64{{3, 2}, {1, 1}, {5, 3}, {0, 4}})
}

// The loop condition computed in the header is read again in the body and in
// the exit block, so it is live-out of the header on both edges.
func TestLoopConditionLiveOut(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 0
    v2 = 0
    jmp head
  head:
    v3 = cmplt v2, v0
    br v3, body, done
  body:
    v4 = add v1, v3
    v5 = 1
    v6 = add v2, v5
    v1 = v4
    v2 = v6
    jmp head
  done:
    v7 = add v1, v3
    ret v7
`
	runEquivPaths(t, src, "main", allK, [][]int64{{5}, {0}, {1}, {10}})
}

// Branches interleaved with loads and stores; memory is fully compared.
func TestBranchWithMemory(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 0
    v2 = 10
    store v0, [v1]
    v3 = cmplt v0, v2
    br v3, small, big
  small:
    v4 = 1
    store v4, [v1 + 1]
    jmp join
  big:
    v5 = 2
    store v5, [v1 + 1]
    jmp join
  join:
    v6 = [v1 + 1]
    v7 = add v6, v3
    ret v7
`
	runEquivPaths(t, src, "main", allK, [][]int64{{3}, {10}, {25}})
}
