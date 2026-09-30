package alloc

import (
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

func (e *emitter) emitInstr(ins *ir.Instr) error {
	e.clearReserved()
	e.scratch = e.scratch[:0]
	switch ins.Op {
	case ir.OpConst:
		dst := e.defineDst(ins.Dst.Reg)
		e.emit(target.MInstr{Op: target.MOpLi, Rd: dst, Imm: ins.Src1.Imm, Line: ins.Line})
	case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpCmpLT:
		if err := e.emitBinary(ins); err != nil {
			return err
		}
	case ir.OpLoad:
		e.reserveUse(ins.Src1)
		addr := e.physOf(ins.Src1)
		dst := e.defineDst(ins.Dst.Reg)
		e.emit(target.MInstr{Op: target.MOpLoad, Rd: dst, Rs1: addr, Line: ins.Line})
	case ir.OpStore:
		e.reserveUse(ins.Src1)
		addr := e.physOf(ins.Src1)
		e.reserveUseDistinct(ins.Src2, addr)
		val := e.physOf(ins.Src2)
		e.emit(target.MInstr{Op: target.MOpStore, Rs1: addr, Rs2: val, Line: ins.Line})
	case ir.OpBr:
		e.emit(target.MInstr{Op: target.MOpB, Label: labelName(e.f.Name, ins.Block), Line: ins.Line})
	case ir.OpBrCond:
		e.reserveUse(ins.Src1)
		cond := e.physOf(ins.Src1)
		e.emit(target.MInstr{
			Op:    target.MOpBnez,
			Rs1:   cond,
			Label: labelName(e.f.Name, ins.Block),
			Func:  labelName(e.f.Name, ins.Func),
			Line:  ins.Line,
		})
	case ir.OpCall:
		if err := e.emitCall(ins); err != nil {
			return err
		}
	case ir.OpRet:
		if err := e.emitRet(ins); err != nil {
			return err
		}
	default:
		return &ir.Error{Line: ins.Line, Msg: "cannot lower instruction"}
	}
	return nil
}

func (e *emitter) emitBinary(ins *ir.Instr) error {
	e.reserveUse(ins.Src1)
	rs1 := e.physOf(ins.Src1)
	e.reserveUseDistinct(ins.Src2, rs1)
	rs2 := e.physOf(ins.Src2)
	e.markReserved(rs1)
	e.markReserved(rs2)
	dst := e.defineDstDistinct(ins.Dst.Reg, rs1, rs2)
	op := map[ir.Op]target.MOp{
		ir.OpAdd:   target.MOpAdd,
		ir.OpSub:   target.MOpSub,
		ir.OpMul:   target.MOpMul,
		ir.OpCmpLT: target.MOpCmpLT,
	}[ins.Op]
	e.emit(target.MInstr{Op: op, Rd: dst, Rs1: rs1, Rs2: rs2, Line: ins.Line})
	e.finishTmp(ins.Src1, rs1)
	e.finishTmp(ins.Src2, rs2)
	return nil
}

func (e *emitter) emitCall(ins *ir.Instr) error {
	if len(ins.Args) > e.cfg.NumPhysical {
		return &ir.Error{Line: ins.Line, Msg: "too many call arguments"}
	}
	// Save every currently-resident value: the callee clobbers all
	// physical registers. Dirty values need stores; clean ones are dropped.
	for pr := 0; pr < e.cfg.NumPhysical; pr++ {
		if !e.free[pr] {
			e.evictFrom(pr)
		}
	}
	e.clearReserved()

	// Load argument i into ri. Since every live value was just made
	// non-resident, any vreg argument must come from its spill slot, which
	// keeps the mapping unambiguous.
	for i, a := range ins.Args {
		if !e.free[i] {
			e.evictFrom(i)
		}
		delete(e.free, i)
		if a.IsReg {
			slot := e.slotFor(a.Reg)
			e.emit(target.MInstr{Op: target.MOpReload, Rd: i, Slot: slot})
			e.stats.Reloads++
			e.inReg[a.Reg] = true
			e.locReg[a.Reg] = i
			e.dirty[a.Reg] = false
		} else {
			e.emit(target.MInstr{Op: target.MOpLi, Rd: i, Imm: a.Imm})
		}
	}

	// r0 receives the result; bind the dst vreg to it.
	if !e.free[0] {
		e.evictFrom(0)
	}
	e.free[0] = false
	e.inReg[ins.Dst.Reg] = true
	e.locReg[ins.Dst.Reg] = 0
	e.dirty[ins.Dst.Reg] = true
	e.emit(target.MInstr{Op: target.MOpCall, Func: ins.Func, Rd: 0, Line: ins.Line})
	return nil
}

func (e *emitter) emitRet(ins *ir.Instr) error {
	e.reserveUse(ins.Src1)
	src := e.physOf(ins.Src1)
	if ins.Src1.IsReg {
		if src != 0 {
			// Free r0 without disturbing the value being returned.
			e.markReserved(src)
			if !e.free[0] {
				e.evictFrom(0)
			}
			e.emit(target.MInstr{Op: target.MOpMov, Rd: 0, Rs1: src, Line: ins.Line})
		}
	} else {
		if !e.free[0] {
			e.evictFrom(0)
		}
		e.emit(target.MInstr{Op: target.MOpLi, Rd: 0, Imm: ins.Src1.Imm, Line: ins.Line})
	}
	e.emit(target.MInstr{Op: target.MOpRet, Rs1: 0, Line: ins.Line})
	return nil
}

// reserveUse pins the operand's register (allocating one and reloading if
// needed) so later operand setup for the same instruction cannot evict it.
func (e *emitter) reserveUse(op ir.Operand) {
	if op.IsReg {
		if !e.inReg[op.Reg] {
			e.assign(op.Reg, true)
		}
		e.markReserved(e.locReg[op.Reg])
		if !e.valueUsedLater(op.Reg) {
			e.dying = append(e.dying, op.Reg)
		}
		return
	}
	// Immediate: allocate a scratch register outside the reserved pool.
	pr := e.freeReg()
	if pr < 0 {
		pr = e.evict()
	}
	delete(e.free, pr)
	e.emit(target.MInstr{Op: target.MOpLi, Rd: pr, Imm: op.Imm})
	e.markReserved(pr)
	e.scratch = append(e.scratch, pr)
}

// physOf returns the physical register currently holding the operand.
func (e *emitter) physOf(op ir.Operand) int {
	if op.IsReg {
		return e.locReg[op.Reg]
	}
	pr := e.scratch[0]
	e.scratch = e.scratch[1:]
	return pr
}

// reserveUseDistinct pins an operand in a register different from other.
func (e *emitter) reserveUseDistinct(op ir.Operand, other int) {
	if op.IsReg && e.inReg[op.Reg] && e.locReg[op.Reg] == other {
		// Same vreg read twice: aliasing is harmless.
		v := e.vregInReg(other)
		if v == op.Reg {
			e.markReserved(other)
			if !e.valueUsedLater(op.Reg) {
				e.dying = append(e.dying, op.Reg)
			}
			return
		}
	}
	// Pin a scratch/register that is not other.
	if op.IsReg && e.inReg[op.Reg] && e.locReg[op.Reg] != other {
		e.reserveUse(op)
		return
	}
	e.markReserved(other)
	if op.IsReg {
		if e.inReg[op.Reg] {
			cur := e.locReg[op.Reg]
			pr := e.freeReg()
			if pr < 0 {
				pr = e.evictExcept(cur)
			}
			delete(e.free, pr)
			e.emit(target.MInstr{Op: target.MOpMov, Rd: pr, Rs1: cur})
			e.inReg[op.Reg] = true
			e.locReg[op.Reg] = pr
			e.dirty[op.Reg] = true
			e.markReserved(pr)
			if !e.valueUsedLater(op.Reg) {
				e.dying = append(e.dying, op.Reg)
			}
			return
		}
		pr := e.assignExcept(op.Reg, true, other)
		e.markReserved(pr)
		if !e.valueUsedLater(op.Reg) {
			e.dying = append(e.dying, op.Reg)
		}
		return
	}
	pr := e.freeReg()
	if pr < 0 {
		pr = e.evictExcept(other)
	}
	delete(e.free, pr)
	e.emit(target.MInstr{Op: target.MOpLi, Rd: pr, Imm: op.Imm})
	e.markReserved(pr)
	e.scratch = append(e.scratch, pr)
}

// finishTmp frees an immediate scratch register after the instruction that
// used it has been emitted.
func (e *emitter) finishTmp(op ir.Operand, pr int) {
	if !op.IsReg && pr >= 0 {
		e.free[pr] = true
	}
}

// defineDst allocates the destination register after all sources are pinned.
func (e *emitter) defineDst(v int) int {
	if e.inReg[v] {
		return e.locReg[v]
	}
	return e.assign(v, false)
}

// defineDstDistinct allocates a destination physical register different from
// both source registers, because the target machine overwrites Rd after
// reading Rs1/Rs2 and aliasing would corrupt an input.
func (e *emitter) defineDstDistinct(v int, rs1, rs2 int) int {
	if e.inReg[v] {
		cur := e.locReg[v]
		if cur != rs1 && cur != rs2 {
			return cur
		}
		// v is redefined, so its old value is dead; only the two sources
		// must remain in their registers. Obtain a register distinct from
		// both sources by spilling some other resident value.
		pr := e.freeReg()
		if pr < 0 || pr == rs1 || pr == rs2 {
			pr = e.evictDistinct(rs1, rs2)
		}
		delete(e.free, pr)
		e.free[cur] = true
		e.inReg[v] = false
		e.locReg[v] = -1
		e.inReg[v] = true
		e.locReg[v] = pr
		e.dirty[v] = true
		return pr
	}
	// Destination not resident: pick a free non-source register.
	pr := e.freeReg()
	if pr < 0 || pr == rs1 || pr == rs2 {
		pr = e.evictDistinct(rs1, rs2)
	}
	delete(e.free, pr)
	e.inReg[v] = true
	e.locReg[v] = pr
	e.dirty[v] = true
	return pr
}
