package ir

import "fmt"

// Def returns the register defined by an instruction and whether it defines one.
func (i *Inst) Def() (Reg, bool) {
	if i.Op.HasResult() {
		return i.Dst, true
	}
	return 0, false
}

// Uses returns every register read by an instruction.
// A store treats its address base (stored in Dst) as a use.
func (i *Inst) Uses() []Reg {
	uses := make([]Reg, 0, len(i.Args)+3)
	switch i.Op {
	case OpStore:
		uses = append(uses, i.Src1, i.Dst)
	case OpLoad, OpBr, OpRet:
		if i.HasSrc1 {
			uses = append(uses, i.Src1)
		}
	case OpConst, OpJmp, OpNop:
		// no register uses
	default:
		if i.HasSrc1 {
			uses = append(uses, i.Src1)
		}
		if i.HasSrc2 {
			uses = append(uses, i.Src2)
		}
	}
	if i.Op == OpCall {
		uses = append(uses, i.Args...)
	}
	return uses
}

// Validate performs semantic validation of a parsed program.
func Validate(p *Program) error {
	for _, f := range p.Funcs {
		if err := validateFunc(p, f); err != nil {
			return err
		}
	}
	return nil
}

func validateFunc(p *Program, f *Func) error {
	if len(f.Blocks) == 0 {
		return &Error{Pos: f.Pos, Msg: fmt.Sprintf("function %q has no blocks", f.Name)}
	}

	// Parameter sanity.
	seenParam := map[Reg]bool{}
	maxReg := Reg(-1)
	for _, r := range f.Params {
		if seenParam[r] {
			return &Error{Pos: f.Pos, Msg: fmt.Sprintf("function %q declares duplicate parameter v%d", f.Name, r)}
		}
		seenParam[r] = true
		if r > maxReg {
			maxReg = r
		}
	}

	// Block terminator checks and register inventory.
	for bi, b := range f.Blocks {
		if len(b.Inst) == 0 {
			return &Error{Pos: b.Pos, Msg: fmt.Sprintf("block %q in function %q is empty", b.Name, f.Name)}
		}
		for ii, ins := range b.Inst {
			last := ii == len(b.Inst)-1
			if ins.Op.IsTerm() && !last {
				return &Error{Pos: ins.Pos, Msg: fmt.Sprintf("terminator %s in block %q is not the last instruction", ins.Op, b.Name)}
			}
			if last && !ins.Op.IsTerm() {
				return &Error{Pos: ins.Pos, Msg: fmt.Sprintf("block %q in function %q does not end with a terminator", b.Name, f.Name)}
			}
			if d, ok := ins.Def(); ok {
				if d > maxReg {
					maxReg = d
				}
			}
			for _, u := range ins.Uses() {
				if u > maxReg {
					maxReg = u
				}
			}
		}

		// Control flow targets.
		term := b.Inst[len(b.Inst)-1]
		switch term.Op {
		case OpJmp:
			if err := checkLabel(f, term.Pos, term.Label, bi); err != nil {
				return err
			}
		case OpBr:
			if err := checkLabel(f, term.Pos, term.Label, bi); err != nil {
				return err
			}
			if err := checkLabel(f, term.Pos, term.Next, bi); err != nil {
				return err
			}
		}
	}

	// Call arity.
	for _, b := range f.Blocks {
		for _, ins := range b.Inst {
			if ins.Op != OpCall {
				continue
			}
			fi, ok := p.FuncIndex(ins.Func)
			if !ok {
				return &Error{Pos: ins.Pos, Msg: fmt.Sprintf("call to undefined function %q", ins.Func)}
			}
			want := len(p.Funcs[fi].Params)
			if len(ins.Args) != want {
				return &Error{Pos: ins.Pos, Msg: fmt.Sprintf("call to %q expects %d arguments, got %d", ins.Func, want, len(ins.Args))}
			}
		}
	}

	return checkDefined(f, int(maxReg)+1)
}

func checkLabel(f *Func, pos Pos, label string, from int) error {
	to, ok := f.BlockIndex(label)
	if !ok {
		return &Error{Pos: pos, Msg: fmt.Sprintf("jump to undefined block %q in function %q", label, f.Name)}
	}
	_ = to
	_ = from
	return nil
}

// checkDefined rejects any use of a vreg that is not defined on every path
// reaching that instruction.
func checkDefined(f *Func, n int) error {
	nb := len(f.Blocks)
	// pred[b] lists predecessor block indices.
	pred := make([][]int, nb)
	for bi, b := range f.Blocks {
		term := b.Inst[len(b.Inst)-1]
		switch term.Op {
		case OpJmp:
			to, _ := f.BlockIndex(term.Label)
			pred[to] = append(pred[to], bi)
		case OpBr:
			t1, _ := f.BlockIndex(term.Label)
			t2, _ := f.BlockIndex(term.Next)
			pred[t1] = append(pred[t1], bi)
			if t2 != t1 {
				pred[t2] = append(pred[t2], bi)
			}
		}
	}

	// in[b]: registers definitely defined on entry to block b.
	in := make([]bitset, nb)
	out := make([]bitset, nb)
	for bi := range f.Blocks {
		in[bi] = newBitset(n)
		out[bi] = allBitset(n)
	}
	entry := newBitset(n)
	for _, r := range f.Params {
		entry.Set(int(r))
	}

	changed := true
	for changed {
		changed = false
		for bi, b := range f.Blocks {
			var inset bitset
			if bi == 0 {
				// Entry parameters are defined at function entry; back-edges
				// never remove that guarantee.
				inset = entry.Clone()
			} else if len(pred[bi]) == 0 {
				inset = allBitset(n)
			} else {
				inset = out[pred[bi][0]].Clone()
				for _, pi := range pred[bi][1:] {
					inset.Intersect(out[pi])
				}
			}
			if !in[bi].Equals(inset) {
				in[bi] = inset
				changed = true
			}
			cur := inset.Clone()
			for _, ins := range b.Inst {
				if d, ok := ins.Def(); ok {
					cur.Set(int(d))
				}
			}
			if !out[bi].Equals(cur) {
				out[bi] = cur
				changed = true
			}
		}
	}

	// Fixed point reached: verify every use with the converged in-sets.
	for _, b := range f.Blocks {
		bi, _ := f.BlockIndex(b.Name)
		cur := in[bi].Clone()
		for _, ins := range b.Inst {
			for _, u := range ins.Uses() {
				if !cur.Get(int(u)) {
					return &Error{Pos: ins.Pos, Msg: fmt.Sprintf("use of undefined value v%d in function %q", u, f.Name)}
				}
			}
			if d, ok := ins.Def(); ok {
				cur.Set(int(d))
			}
		}
	}
	return nil
}

type bitset []uint64

func newBitset(n int) bitset {
	return make(bitset, (n+63)/64)
}

func allBitset(n int) bitset {
	b := newBitset(n)
	for i := 0; i < n; i++ {
		b.Set(i)
	}
	return b
}

func (b bitset) Set(i int) { b[i/64] |= 1 << uint(i%64) }
func (b bitset) Get(i int) bool {
	return b[i/64]&(1<<uint(i%64)) != 0
}

func (b bitset) Clone() bitset {
	c := make(bitset, len(b))
	copy(c, b)
	return c
}

func (b bitset) Intersect(o bitset) {
	for i := range b {
		b[i] &= o[i]
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
