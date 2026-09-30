package alloc

import (
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// emitter is the per-function register allocator and code emitter.
type emitter struct {
	f        *ir.Function
	cfg      Config
	lr       *LiveRanges
	nv       int
	out      []target.MInstr
	nextSlot int
	stats    Stats
	locReg   []int
	locSlot  []int
	inReg    []bool
	dirty    []bool
	free     map[int]bool
	reserved map[int]bool
	scratch  []int
	dying    []int
	idx      int
	curInstr *ir.Instr
}

func newEmitter(f *ir.Function, cfg Config, lr *LiveRanges, nv int) *emitter {
	nr := cfg.NumPhysical
	e := &emitter{
		f:        f,
		cfg:      cfg,
		lr:       lr,
		nv:       nv,
		locReg:   make([]int, nv),
		locSlot:  make([]int, nv),
		inReg:    make([]bool, nv),
		dirty:    make([]bool, nv),
		free:     map[int]bool{},
		reserved: map[int]bool{},
	}
	for r := 0; r < nr; r++ {
		e.free[r] = true
	}
	for v := 0; v < nv; v++ {
		e.locReg[v] = -1
		e.locSlot[v] = -1
	}
	return e
}

func (e *emitter) emit(ins target.MInstr) {
	e.out = append(e.out, ins)
}

// slotFor assigns a spill home slot to a virtual register deterministically.
func (e *emitter) slotFor(v int) int {
	if e.locSlot[v] < 0 {
		e.locSlot[v] = e.nextSlot
		e.nextSlot++
	}
	return e.locSlot[v]
}

func (e *emitter) run() (*target.MFunction, Stats, error) {
	// Parameters arrive in r0..r(k-1) and are live at function entry.
	for i, v := range e.f.Params {
		if i >= e.cfg.NumPhysical {
			return nil, Stats{}, &ir.Error{Msg: "too many parameters for physical registers"}
		}
		delete(e.free, i)
		e.inReg[v] = true
		e.locReg[v] = i
		e.dirty[v] = true
	}

	for _, b := range e.f.Blocks {
		e.emit(target.MInstr{Op: target.MOpLabel, Label: labelName(e.f.Name, b.Name)})
		if b != e.f.Blocks[0] {
			e.resetAtEntry()
		}
		for _, ins := range b.Instrs {
			e.idx = e.flatIndex(ins)
			e.curInstr = ins
			e.dying = e.dying[:0]
			terminator := ins.Op == ir.OpBr || ins.Op == ir.OpBrCond || ins.Op == ir.OpRet
			if terminator {
				// At a control-flow boundary every value live after this
				// instruction must live in its spill slot, giving all
				// successors one canonical incoming state.
				if ins.Op != ir.OpRet {
					e.flushLiveAfter()
				}
			}
			if err := e.emitInstr(ins); err != nil {
				return nil, Stats{}, err
			}
			for _, pr := range e.scratch {
				e.free[pr] = true
			}
			e.scratch = e.scratch[:0]
			for _, v := range e.dying {
				if e.inReg[v] {
					pr := e.locReg[v]
					e.free[pr] = true
					e.inReg[v] = false
					e.locReg[v] = -1
					e.dirty[v] = false
				}
			}
			e.dying = e.dying[:0]
		}
	}

	mf := &target.MFunction{
		Name:       e.f.Name,
		NumParams:  len(e.f.Params),
		StackSlots: e.nextSlot,
		Instrs:     e.out,
		LiveAtExit: append([]bool(nil), e.lr.LiveAtExit...),
	}
	for v := 0; v < e.nv; v++ {
		loc := target.VLoc{VReg: v}
		if e.inReg[v] {
			loc.InReg = true
			loc.Reg = e.locReg[v]
		} else if e.locSlot[v] >= 0 {
			loc.Slot = e.locSlot[v]
		} else {
			continue
		}
		mf.FinalLocs = append(mf.FinalLocs, loc)
	}
	return mf, e.stats, nil
}

func labelName(fn, block string) string {
	return fn + "$" + block
}

func (e *emitter) flatIndex(target *ir.Instr) int {
	off := 0
	for _, b := range e.f.Blocks {
		for _, ins := range b.Instrs {
			if ins == target {
				return off
			}
			off++
		}
	}
	return -1
}
