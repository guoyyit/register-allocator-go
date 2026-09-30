package alloc

import (
	"github.com/guoyyit/register-allocator-go/internal/ir"
)

// LiveSets holds liveness bit sets per instruction.
type LiveSets struct {
	// LiveIn[i][b] is live-in vregs for block b, instruction i.
	LiveIn [][]bool
	// LiveOut[i][b] is live-out vregs for block b, instruction i.
	LiveOut [][]bool
	NumRegs int
}

// Analyze computes per-instruction liveness for one IR function.
func Analyze(f *ir.Func, numVRegs int) *LiveSets {
	nb := len(f.Blocks)
	n := numVRegs

	succ := make([][]int, nb)
	for bi, b := range f.Blocks {
		term := b.Inst[len(b.Inst)-1]
		switch term.Op {
		case ir.OpJmp:
			to, _ := f.BlockIndex(term.Label)
			succ[bi] = append(succ[bi], to)
		case ir.OpBr:
			t1, _ := f.BlockIndex(term.Label)
			t2, _ := f.BlockIndex(term.Next)
			succ[bi] = append(succ[bi], t1)
			if t2 != t1 {
				succ[bi] = append(succ[bi], t2)
			}
		}
	}

	blockIn := make([]bitset, nb)
	blockOut := make([]bitset, nb)
	for bi := range f.Blocks {
		blockIn[bi] = newBS(n)
		blockOut[bi] = newBS(n)
	}

	changed := true
	for changed {
		changed = false
		for bi := nb - 1; bi >= 0; bi-- {
			b := f.Blocks[bi]
			out := newBS(n)
			for _, s := range succ[bi] {
				out.Union(blockIn[s])
			}
			in := out.Clone()
			for ii := len(b.Inst) - 1; ii >= 0; ii-- {
				ins := b.Inst[ii]
				if d, ok := ins.Def(); ok {
					in.Clear(int(d))
				}
				for _, u := range ins.Uses() {
					in.Set(int(u))
				}
			}
			if !blockIn[bi].Equals(in) || !blockOut[bi].Equals(out) {
				blockIn[bi] = in
				blockOut[bi] = out
				changed = true
			}
		}
	}

	ls := &LiveSets{NumRegs: n}
	totalInsts := 0
	for _, b := range f.Blocks {
		totalInsts += len(b.Inst)
	}
	ls.LiveIn = make([][]bool, nb)
	ls.LiveOut = make([][]bool, nb)
	for bi, b := range f.Blocks {
		ls.LiveIn[bi] = make([]bool, len(b.Inst)*n)
		ls.LiveOut[bi] = make([]bool, len(b.Inst)*n)
		cur := blockOut[bi].Clone()
		for ii := len(b.Inst) - 1; ii >= 0; ii-- {
			ins := b.Inst[ii]
			for r := 0; r < n; r++ {
				ls.LiveOut[bi][ii*n+r] = cur.Get(r)
			}
			if d, ok := ins.Def(); ok {
				cur.Clear(int(d))
			}
			for _, u := range ins.Uses() {
				cur.Set(int(u))
			}
			for r := 0; r < n; r++ {
				ls.LiveIn[bi][ii*n+r] = cur.Get(r)
			}
		}
	}
	return ls
}

// LiveInAt reports whether vreg r is live just before b/inst.
func (l *LiveSets) LiveInAt(b, inst, r int) bool {
	return l.LiveIn[b][inst*l.NumRegs+r]
}

// LiveOutAt reports whether vreg r is live just after b/inst.
func (l *LiveSets) LiveOutAt(b, inst, r int) bool {
	return l.LiveOut[b][inst*l.NumRegs+r]
}

type bitset []uint64

func newBS(n int) bitset { return make(bitset, (n+63)/64) }

func (b bitset) Set(i int)   { b[i/64] |= 1 << uint(i%64) }
func (b bitset) Clear(i int) { b[i/64] &^= 1 << uint(i%64) }
func (b bitset) Get(i int) bool {
	return b[i/64]&(1<<uint(i%64)) != 0
}

func (b bitset) Clone() bitset {
	c := make(bitset, len(b))
	copy(c, b)
	return c
}

func (b bitset) Union(o bitset) {
	for i := range b {
		b[i] |= o[i]
	}
}

func (b bitset) Equals(o bitset) bool {
	for i := range b {
		if b[i] != o[i] {
			return false
		}
	}
	return true
}
