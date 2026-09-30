package alloc

import (
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// markReserved holds a physical register out of the eviction pool while an
// instruction's operands are being prepared.
func (e *emitter) markReserved(pr int) {
	if pr >= 0 {
		e.reserved[pr] = true
	}
}

func (e *emitter) clearReserved() {
	e.reserved = map[int]bool{}
}

func (e *emitter) vregInReg(pr int) int {
	for v := 0; v < e.nv; v++ {
		if e.inReg[v] && e.locReg[v] == pr {
			return v
		}
	}
	return -1
}

// evictFrom frees physical register pr by spilling its occupant to its home
// slot. Dirty occupants must be stored; clean ones only need state updates.
func (e *emitter) evictFrom(pr int) {
	v := e.vregInReg(pr)
	if v < 0 {
		e.free[pr] = true
		return
	}
	if e.dirty[v] {
		slot := e.slotFor(v)
		e.emit(target.MInstr{Op: target.MOpSpill, Rd: pr, Slot: slot})
		e.stats.Spills++
		e.dirty[v] = false
	}
	e.inReg[v] = false
	e.locReg[v] = -1
	e.free[pr] = true
}

func (e *emitter) freeReg() int {
	for pr := 0; pr < e.cfg.NumPhysical; pr++ {
		if e.free[pr] && !e.reserved[pr] {
			return pr
		}
	}
	return -1
}

// evict selects the occupied, non-reserved register whose current value is
// used furthest in the future (or never used again) and spills it.
// Tie-break is deterministic: highest physical register number.
func (e *emitter) evict() int {
	return e.evictExcept(-1)
}

// evictDistinct returns a free physical register different from both keep
// registers, evicting the best non-protected occupant if necessary.
func (e *emitter) evictDistinct(keep1, keep2 int) int {
	for pr := 0; pr < e.cfg.NumPhysical; pr++ {
		if pr == keep1 || pr == keep2 {
			continue
		}
		if e.free[pr] {
			return pr
		}
	}
	bestReg := -1
	bestUse := -2
	for pr := 0; pr < e.cfg.NumPhysical; pr++ {
		if pr == keep1 || pr == keep2 {
			continue
		}
		v := e.vregInReg(pr)
		if v < 0 {
			e.free[pr] = true
			return pr
		}
		nu := e.useDistance(v)
		if bestReg == -1 || nu > bestUse || (nu == bestUse && pr > bestReg) {
			bestReg, bestUse = pr, nu
		}
	}
	if bestReg < 0 {
		return -1
	}
	e.evictFrom(bestReg)
	return bestReg
}

// evictExcept evicts a register, but never evicts protected physical regs
// unless all non-protected registers are unavailable. Values whose next use
// is the current instruction (dying sources) are evicted first.
func (e *emitter) evictExcept(protected int) int {
	bestReg := -1
	bestUse := -2
	for pr := 0; pr < e.cfg.NumPhysical; pr++ {
		if e.free[pr] {
			continue
		}
		v := e.vregInReg(pr)
		if v < 0 {
			// An unowned occupied register (e.g. a scratch register whose
			// reservation lapsed) is immediately reusable.
			e.free[pr] = true
			if pr != protected {
				return pr
			}
			continue
		}
		if pr == protected {
			continue
		}
		nu := e.useDistance(v)
		if e.reserved[pr] && e.lr.NextUse[e.idx][v] != e.idx {
			continue
		}
		if bestReg == -1 || nu > bestUse || (nu == bestUse && pr > bestReg) {
			bestReg, bestUse = pr, nu
		}
	}
	if bestReg < 0 {
		// Fall back to evicting a reserved register anyway; the value must
		// have a home slot and it can be reloaded for its own use.
		for pr := 0; pr < e.cfg.NumPhysical; pr++ {
			if e.free[pr] || pr == protected {
				continue
			}
			v := e.vregInReg(pr)
			if v < 0 {
				e.free[pr] = true
				return pr
			}
			nu := e.useDistance(v)
			if bestReg < 0 || nu > bestUse || (nu == bestUse && pr > bestReg) {
				bestReg, bestUse = pr, nu
			}
		}
	}
	if bestReg < 0 {
		return -1
	}
	e.evictFrom(bestReg)
	return bestReg
}

// assign puts virtual register v into a free physical register, reloading its
// value from its spill slot if needed. protected is never chosen.
func (e *emitter) assign(v int, needValue bool) int {
	return e.assignExcept(v, needValue, -1)
}

func (e *emitter) assignExcept(v int, needValue bool, protected int) int {
	pr := e.freeReg()
	if pr < 0 {
		pr = e.evictExcept(protected)
	}
	delete(e.free, pr)
	e.inReg[v] = true
	e.locReg[v] = pr
	if needValue && !e.dirty[v] && e.locSlot[v] >= 0 {
		e.emit(target.MInstr{Op: target.MOpReload, Rd: pr, Slot: e.locSlot[v]})
		e.stats.Reloads++
		e.dirty[v] = true
	} else if !needValue {
		e.dirty[v] = true
	}
	return pr
}

// valueUsedLater reports whether v is observable at or after the current
// point: either by a reachable use captured by next-use analysis, or by being
// live-out of the current instruction across a control-flow edge.
func (e *emitter) valueUsedLater(v int) bool {
	if e.lr.LiveOut[e.idx][v] {
		return true
	}
	return e.lr.NextUse[e.idx][v] >= 0
}

// useDistance is the eviction priority: larger means used further in the
// future. A value live across a CFG edge with no local use gets a large but
// finite distance so cross-edge-live values are spilled after dying values.
func (e *emitter) useDistance(v int) int {
	if nu := e.lr.NextUse[e.idx][v]; nu >= 0 {
		return nu - e.idx
	}
	if e.lr.LiveOut[e.idx][v] {
		return len(e.lr.NextUse) + 1 + v
	}
	return -1
}

// flushLiveAfter spills every resident value that is observable after the
// current terminator instruction. Operands of the terminator stay resident.
func (e *emitter) flushLiveAfter() {
	for v := 0; v < e.nv; v++ {
		if !e.inReg[v] || !e.dirty[v] {
			continue
		}
		// The terminator's operand stays resident; every other value that
		// is live after this point must be written to its spill slot.
		isOperand := false
		for _, u := range irUses(e.currentInstr()) {
			if u.IsReg && u.Reg == v {
				isOperand = true
			}
		}
		if isOperand {
			continue
		}
		if !e.lr.LiveOut[e.idx][v] {
			continue
		}
		pr := e.locReg[v]
		slot := e.slotFor(v)
		e.emit(target.MInstr{Op: target.MOpSpill, Rd: pr, Slot: slot})
		e.stats.Spills++
		e.dirty[v] = false
	}
}

func (e *emitter) currentInstr() *ir.Instr { return e.curInstr }

// resetAtEntry restores the canonical block-entry state: every register is
// free, and resident values carried over a fallthrough path are dropped (their
// authoritative copies live in the spill slots written by predecessors).
func (e *emitter) resetAtEntry() {
	for v := 0; v < e.nv; v++ {
		if e.inReg[v] {
			pr := e.locReg[v]
			e.free[pr] = true
			e.inReg[v] = false
			e.locReg[v] = -1
			// The authoritative value is now whatever a predecessor wrote
			// to the spill slot; the in-register copy is stale by definition.
			e.dirty[v] = false
		}
	}
}
