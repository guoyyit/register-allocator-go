package codegen

import (
	"fmt"

	"github.com/guoyyit/register-allocator-go/internal/alloc"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// Result is generated code plus statistics.
type Result struct {
	Program *target.Program
	Stats   target.Stats
}

// Generate lowers an IR program to deterministic target code.
func Generate(p *ir.Program, k int) (*Result, error) {
	if k < 2 {
		return nil, fmt.Errorf("codegen: need at least 2 physical registers, got %d", k)
	}
	if err := ir.Validate(p); err != nil {
		return nil, err
	}
	if err := alloc.CheckTooManyParams(p, k); err != nil {
		return nil, err
	}
	analysis, err := alloc.New(k).Allocate(p)
	if err != nil {
		return nil, err
	}

	res := &Result{Program: &target.Program{NumRegs: k}}
	cg := &generator{
		k:       k,
		program: p,
		stats:   &res.Stats,
	}
	for fi, f := range p.Funcs {
		tf, err := cg.genFunc(f, analysis.Funcs[fi])
		if err != nil {
			return nil, err
		}
		res.Program.Funcs = append(res.Program.Funcs, tf)
	}
	return res, nil
}

// generator carries allocation state while emitting one function.
type generator struct {
	k       int
	program *ir.Program
	stats   *target.Stats

	f     *ir.Func
	as    *alloc.Assignment
	n     int
	out   []target.Instruction
	slots int

	// home[v] is the fixed frame slot that backs vreg v, or -1.
	home []int

	// Block-scoped register cache: cachedReg[v] is the register holding a
	// current copy of v, or -1; slotFresh[v] says its slot copy is current.
	cachedReg []int
	slotFresh []bool
}

func (cg *generator) newFunc(f *ir.Func, as *alloc.Assignment) {
	cg.f = f
	cg.as = as
	cg.n = as.NumVRegs
	cg.out = nil
	cg.slots = 0
	cg.home = make([]int, cg.n)
	for v := range cg.home {
		cg.home[v] = -1
	}
	// cachedReg[v] is the register holding a current copy of v within the
	// current block, or -1. cachedSlotFresh[v] says whether its slot copy is
	// also current.
	cg.cachedReg = make([]int, cg.n)
	cg.slotFresh = make([]bool, cg.n)
}

func (cg *generator) emit(ins target.Instruction) {
	cg.out = append(cg.out, ins)
	switch ins.Op {
	case target.OpSpill:
		cg.stats.Spills++
	case target.OpReload:
		cg.stats.Reloads++
	}
}

// allocSlot reserves a new fixed spill slot for a vreg.
func (cg *generator) allocSlot() int {
	s := cg.slots
	cg.slots++
	return s
}

// homeSlot returns the fixed slot backing v, allocating it on first use.
func (cg *generator) homeSlot(v int) int {
	if cg.home[v] < 0 {
		cg.home[v] = cg.allocSlot()
	}
	return cg.home[v]
}

func (cg *generator) genFunc(f *ir.Func, as *alloc.Assignment) (*target.Func, error) {
	cg.newFunc(f, as)
	tf := &target.Func{Name: f.Name, Params: len(f.Params)}

	for bi, b := range f.Blocks {
		tb := &target.Block{Name: b.Name}
		cg.beginBlock(bi == 0)
		for ii, ins := range b.Inst {
			if err := cg.genInst(bi, ii, ins); err != nil {
				return nil, err
			}
		}
		tb.Inst = append(tb.Inst, cg.out...)
		cg.out = nil
		tf.Blocks = append(tf.Blocks, tb)
	}
	return tf, nil
}

// beginBlock resets the block-scoped register cache. On function entry the
// ABI parameter registers are homed into each parameter's fixed slot.
func (cg *generator) beginBlock(isEntry bool) {
	for v := range cg.cachedReg {
		cg.cachedReg[v] = -1
	}
	for v := range cg.slotFresh {
		cg.slotFresh[v] = false
	}
	if !isEntry {
		for v, s := range cg.home {
			if s >= 0 {
				cg.slotFresh[v] = true
			}
		}
		return
	}
	for i, pr := range cg.f.Params {
		v := int(pr)
		s := cg.homeSlot(v)
		cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(i), Slot: s})
		cg.slotFresh[v] = true
	}
}

// regToVReg returns the vreg currently cached in physical register r, or -1.
func (cg *generator) regToVReg(r int) int {
	for v, rr := range cg.cachedReg {
		if rr == r {
			return v
		}
	}
	return -1
}

// freeReg returns an unused physical register outside reserved, or -1.
func (cg *generator) freeReg(reserved map[int]bool) int {
	for r := 0; r < cg.k; r++ {
		if !reserved[r] && cg.regToVReg(r) < 0 {
			return r
		}
	}
	return -1
}

// evict frees a register outside reserved, spilling its cached vreg when the
// value is needed later and its slot copy is not current.
func (cg *generator) evict(bi, ii int, reserved map[int]bool) int {
	if r := cg.freeReg(reserved); r >= 0 {
		return r
	}
	best, bestNext := -1, -1
	for r := 0; r < cg.k; r++ {
		if reserved[r] {
			continue
		}
		v := cg.regToVReg(r)
		if v < 0 {
			return r
		}
		next := cg.nextUse(bi, ii, v)
		if best == -1 || next > bestNext {
			best, bestNext = r, next
		}
	}
	if best < 0 {
		best = 0
	}
	v := cg.regToVReg(best)
	if v >= 0 && cg.liveAfter(bi, ii, v) && !cg.slotFresh[v] {
		cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(best), Slot: cg.homeSlot(v)})
		cg.slotFresh[v] = true
	}
	if v >= 0 {
		cg.cachedReg[v] = -1
	}
	return best
}

// nextUse returns the next in-block instruction index reading vreg v.
func (cg *generator) liveAfter(bi, ii, v int) bool {
	return cg.as.Live.LiveOutAt(bi, ii, v)
}

func (cg *generator) nextUse(bi, ii, v int) int {
	b := cg.f.Blocks[bi]
	for j := ii; j < len(b.Inst); j++ {
		for _, u := range b.Inst[j].Uses() {
			if int(u) == v {
				return j
			}
		}
	}
	return len(b.Inst) + 1
}

// load places vreg v in a register outside reserved, reusing a cached copy
// when possible and otherwise reloading from its home slot.
func (cg *generator) load(bi, ii, v int, reserved map[int]bool) target.Reg {
	if r := cg.cachedReg[v]; r >= 0 && !reserved[r] {
		return target.Reg(r)
	}
	r := cg.evict(bi, ii, reserved)
	cg.emit(target.Instruction{Op: target.OpReload, Rd: target.Reg(r), Slot: cg.homeSlot(v)})
	cg.cachedReg[v] = r
	return target.Reg(r)
}

// define records register r as holding the freshly defined vreg v, writing
// the value to its home slot lazily (on eviction or a control-flow edge).
func (cg *generator) define(bi, ii, v, r int) {
	// Capture the register's current occupant before mutating the cache.
	occupant := cg.regToVReg(r)
	// v's previous register binding is superseded by this definition.
	cg.cachedReg[v] = -1
	// The destination register may currently cache some other vreg; that
	// copy is overwritten and must be forgotten.
	if occupant >= 0 && occupant != v {
		if cg.liveAfter(bi, ii, occupant) && !cg.slotFresh[occupant] {
			cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(r), Slot: cg.homeSlot(occupant)})
			cg.slotFresh[occupant] = true
		}
		cg.cachedReg[occupant] = -1
	}
	cg.cachedReg[v] = r
	// A fresh definition makes any prior slot copy stale; the value is now
	// held only in r until eviction or a block/call boundary.
	cg.slotFresh[v] = false
	_ = cg.homeSlot(v)
}

// flushAtEdge writes cached live values with stale slot copies to their slots.
func (cg *generator) flushAtEdge(bi, ii int, keep *int) {
	for r := 0; r < cg.k; r++ {
		v := cg.regToVReg(r)
		if v < 0 || (keep != nil && *keep == v) {
			continue
		}
		if cg.liveAfter(bi, ii, v) && !cg.slotFresh[v] {
			cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(r), Slot: cg.homeSlot(v)})
			cg.slotFresh[v] = true
		}
	}
}

// syncSlot writes vreg v to its home slot if its cached register copy is the
// only current value, then invalidates its register cache.
func (cg *generator) syncSlot(v int, r int) {
	if cg.regToVReg(r) != v {
		// The register no longer holds v (stale mapping); v's slot is the
		// only authoritative location, so nothing needs writing.
		cg.cachedReg[v] = -1
		return
	}
	if !cg.slotFresh[v] {
		cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(r), Slot: cg.homeSlot(v)})
		cg.slotFresh[v] = true
	}
	cg.cachedReg[v] = -1
}

// prepBinary arranges two sources and a destination for a binary op and
// returns the destination physical register.
//
// Both sources are first made current in their home slots. Every other live
// value occupying a register is spilled, so the subsequent fixed register
// layout cannot clobber anything. With k >= 3 the operands use r0/r1 and the
// result uses r2 (or another free register). With k == 2 the operands use r0
// and r1 and the result lands in r0.
func (cg *generator) prepBinary(bi, ii, s1, s2, d int, op ir.Op) target.Reg {
	if r := cg.cachedReg[s1]; r >= 0 {
		cg.syncSlot(s1, r)
	}
	if r := cg.cachedReg[s2]; r >= 0 {
		cg.syncSlot(s2, r)
	}
	// Forget the sources' register bindings: they are now slot-resident and
	// will be reloaded below. This prevents regToVReg from misattributing a
	// register after reload.
	cg.cachedReg[s1] = -1
	cg.cachedReg[s2] = -1

	operand := map[int]bool{s1: true, s2: true, d: true}
	for r := 0; r < cg.k; r++ {
		v := cg.regToVReg(r)
		if v < 0 || operand[v] {
			continue
		}
		if cg.liveAfter(bi, ii, v) && !cg.slotFresh[v] {
			cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(r), Slot: cg.homeSlot(v)})
			cg.slotFresh[v] = true
		}
		cg.cachedReg[v] = -1
	}

	s1Slot := cg.homeSlot(s1)
	s2Slot := cg.homeSlot(s2)

	if cg.k >= 3 {
		cg.emit(target.Instruction{Op: target.OpReload, Rd: 0, Slot: s1Slot})
		cg.emit(target.Instruction{Op: target.OpReload, Rd: 1, Slot: s2Slot})
		cg.cachedReg[s1] = 0
		cg.cachedReg[s2] = 1
		rd := cg.acquireScratch(bi, ii, map[int]bool{0: true, 1: true})
		cg.emit(target.Instruction{Op: mapBinary(op), Rd: target.Reg(rd), Rs1: 0, Rs2: 1})
		return target.Reg(rd)
	}

	cg.emit(target.Instruction{Op: target.OpReload, Rd: 1, Slot: s2Slot})
	cg.emit(target.Instruction{Op: target.OpReload, Rd: 0, Slot: s1Slot})
	cg.cachedReg[s1] = 0
	cg.cachedReg[s2] = 1
	cg.emit(target.Instruction{Op: mapBinary(op), Rd: 0, Rs1: 0, Rs2: 1})
	return 0
}

func (cg *generator) acquireScratch(bi, ii int, reserved map[int]bool) int {
	for r := 0; r < cg.k; r++ {
		if !reserved[r] && cg.regToVReg(r) < 0 {
			return r
		}
	}
	for r := 0; r < cg.k; r++ {
		if reserved[r] {
			continue
		}
		v := cg.regToVReg(r)
		if v >= 0 && cg.liveAfter(bi, ii, v) && !cg.slotFresh[v] {
			cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(r), Slot: cg.homeSlot(v)})
			cg.slotFresh[v] = true
		}
		if v >= 0 {
			cg.cachedReg[v] = -1
		}
		return r
	}
	panic("acquireScratch: no register available")
}

// genInst lowers one IR instruction using the block-scoped register cache.
func (cg *generator) genInst(bi, ii int, ins *ir.Inst) error {
	switch ins.Op {
	case ir.OpNop:
		cg.emit(target.Instruction{Op: target.OpNop})

	case ir.OpConst:
		r := cg.evict(bi, ii, nil)
		cg.emit(target.Instruction{Op: target.OpLi, Rd: target.Reg(r), Imm: ins.Imm})
		cg.define(bi, ii, int(ins.Dst), r)

	case ir.OpMove:
		src, d := int(ins.Src1), int(ins.Dst)
		// Make src's slot current, then copy it into a scratch register for
		// d. Keeping src in its slot preserves it regardless of d's lifetime.
		if sr := cg.cachedReg[src]; sr >= 0 && !cg.slotFresh[src] {
			cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(sr), Slot: cg.homeSlot(src)})
			cg.slotFresh[src] = true
		}
		rd := cg.evict(bi, ii, nil)
		cg.emit(target.Instruction{Op: target.OpReload, Rd: target.Reg(rd), Slot: cg.homeSlot(src)})
		cg.define(bi, ii, d, rd)

	case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod,
		ir.OpCmpLt, ir.OpCmpLe, ir.OpCmpEq, ir.OpCmpNe:
		rd := cg.prepBinary(bi, ii, int(ins.Src1), int(ins.Src2), int(ins.Dst), ins.Op)
		cg.define(bi, ii, int(ins.Dst), int(rd))

	case ir.OpLoad:
		base := cg.load(bi, ii, int(ins.Src1), nil)
		rd := cg.evict(bi, ii, map[int]bool{int(base): true})
		cg.emit(target.Instruction{Op: target.OpLoad, Rd: target.Reg(rd), Rs1: base, Imm: ins.Imm})
		cg.define(bi, ii, int(ins.Dst), rd)

	case ir.OpStore:
		data := cg.load(bi, ii, int(ins.Src1), nil)
		base := cg.load(bi, ii, int(ins.Dst), map[int]bool{int(data): true})
		cg.emit(target.Instruction{Op: target.OpStore, Rs1: data, Rd: base, Imm: ins.Imm})

	case ir.OpCall:
		cg.genCall(bi, ii, ins)

	case ir.OpRet:
		if ins.HasSrc1 {
			r := cg.load(bi, ii, int(ins.Src1), nil)
			if r != 0 {
				cg.emit(target.Instruction{Op: target.OpMov, Rd: 0, Rs1: r})
			}
		} else {
			cg.emit(target.Instruction{Op: target.OpLi, Rd: 0, Imm: 0})
		}
		cg.emit(target.Instruction{Op: target.OpRet})

	case ir.OpJmp:
		cg.flushAtEdge(bi, ii, nil)
		cg.emit(target.Instruction{Op: target.OpJmp, Label: ins.Label})

	case ir.OpBr:
		cv := int(ins.Src1)
		cg.flushAtEdge(bi, ii, &cv)
		rc := cg.load(bi, ii, cv, nil)
		cg.emit(target.Instruction{Op: target.OpBr, Rs1: rc, Label: ins.Label, Next: ins.Next})

	default:
		return &ir.Error{Pos: ins.Pos, Msg: fmt.Sprintf("codegen: unsupported opcode %s", ins.Op)}
	}
	return nil
}

func mapBinary(op ir.Op) target.Op {
	switch op {
	case ir.OpAdd:
		return target.OpAdd
	case ir.OpSub:
		return target.OpSub
	case ir.OpMul:
		return target.OpMul
	case ir.OpDiv:
		return target.OpDiv
	case ir.OpMod:
		return target.OpMod
	case ir.OpCmpLt:
		return target.OpCmpLt
	case ir.OpCmpLe:
		return target.OpCmpLe
	case ir.OpCmpEq:
		return target.OpCmpEq
	case ir.OpCmpNe:
		return target.OpCmpNe
	}
	return target.OpNop
}

// genCall implements the fixed calling convention: arguments are passed in
// r0..r(n-1), every other live value is spilled across the call, and the
// return value arrives in r0.
func (cg *generator) genCall(bi, ii int, ins *ir.Inst) {
	cfi, _ := cg.program.FuncIndex(ins.Func)
	nargs := len(cg.program.Funcs[cfi].Params)

	// Preserve every value that stays live across the call by making its slot
	// copy current. Argument vregs are handled explicitly below.
	argSet := make(map[int]bool, nargs)
	for _, a := range ins.Args {
		argSet[int(a)] = true
	}
	for r := 0; r < cg.k; r++ {
		v := cg.regToVReg(r)
		if v < 0 {
			continue
		}
		// Spill when the value survives the call, or when it is an argument
		// whose slot must hold the value for the fixed ABI reload below.
		needFresh := cg.liveAfter(bi, ii, v) || argSet[v]
		if needFresh && !cg.slotFresh[v] {
			cg.emit(target.Instruction{Op: target.OpSpill, Rs1: target.Reg(r), Slot: cg.homeSlot(v)})
			cg.slotFresh[v] = true
		}
		cg.cachedReg[v] = -1
	}
	// Arguments that are not register-resident already live in their slot.
	// Ensure every argument slot is populated even if the value was only ever
	// produced and its binding was dropped: nothing to do in that case because
	// a value that survives to this call was spilled on eviction/edge.

	// Load arguments into fixed ABI registers from their slots.
	for i := 0; i < nargs; i++ {
		av := int(ins.Args[i])
		cg.emit(target.Instruction{Op: target.OpReload, Rd: target.Reg(i), Slot: cg.homeSlot(av)})
	}

	cg.emit(target.Instruction{Op: target.OpCall, Func: ins.Func})

	// All registers are caller-saved; only r0 holds the result.
	dst := int(ins.Dst)
	if cg.liveAfter(bi, ii, dst) {
		cg.emit(target.Instruction{Op: target.OpSpill, Rs1: 0, Slot: cg.homeSlot(dst)})
		cg.slotFresh[dst] = true
		cg.cachedReg[dst] = -1
	} else {
		cg.cachedReg[dst] = 0
	}
}
