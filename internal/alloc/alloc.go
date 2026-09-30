package alloc

import (
	"fmt"

	"github.com/guoyyit/register-allocator-go/internal/ir"
)

// Assignment is the per-function liveness information used by code generation.
type Assignment struct {
	Func     *ir.Func
	Live     *LiveSets
	NumVRegs int
	// MaxPressure is the highest number of simultaneously live vregs
	// observed at any instruction boundary.
	MaxPressure int
}

// Result groups all per-function assignments.
type Result struct {
	Funcs []*Assignment
}

// Allocator assigns physical registers, spilling when pressure exceeds k.
type Allocator struct {
	k int
}

// New creates an allocator targeting k physical registers.
func New(k int) *Allocator {
	return &Allocator{k: k}
}

// Allocate runs liveness analysis over every function.
func (a *Allocator) Allocate(p *ir.Program) (*Result, error) {
	res := &Result{}
	for _, f := range p.Funcs {
		n := VRegCount(f)
		live := Analyze(f, n)
		as := &Assignment{
			Func:     f,
			Live:     live,
			NumVRegs: n,
		}
		for bi, b := range f.Blocks {
			for ii := range b.Inst {
				count := 0
				for r := 0; r < n; r++ {
					if live.LiveInAt(bi, ii, r) {
						count++
					}
				}
				if count > as.MaxPressure {
					as.MaxPressure = count
				}
			}
		}
		res.Funcs = append(res.Funcs, as)
	}
	return res, nil
}

// K returns the physical register count.
func (a *Allocator) K() int { return a.k }

// VRegCount returns one more than the largest vreg id used by f.
func VRegCount(f *ir.Func) int {
	max := -1
	note := func(r ir.Reg) {
		if int(r) > max {
			max = int(r)
		}
	}
	for _, r := range f.Params {
		note(r)
	}
	for _, b := range f.Blocks {
		for _, ins := range b.Inst {
			if d, ok := ins.Def(); ok {
				note(d)
			}
			for _, u := range ins.Uses() {
				note(u)
			}
		}
	}
	if max < 0 {
		return 0
	}
	return max + 1
}

// CheckTooManyParams reports whether any function has more parameters
// than the machine can pass in registers.
func CheckTooManyParams(p *ir.Program, k int) error {
	for _, f := range p.Funcs {
		if len(f.Params) > k {
			return &ir.Error{
				Pos: f.Pos,
				Msg: fmt.Sprintf("function %q takes %d parameters but the calling convention provides only %d registers", f.Name, len(f.Params), k),
			}
		}
	}
	return nil
}
