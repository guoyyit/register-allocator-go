package alloc

import "github.com/guoyyit/register-allocator-go/internal/ir"

// LiveRanges holds liveness and next-use information for a function.
type LiveRanges struct {
	NumVRegs int
	// LiveAtExit are the virtual registers observable at function return.
	LiveAtExit []bool
	// LiveIn[i] is the set live immediately before executing instruction i
	// over the function's flattened instruction order.
	LiveIn [][]bool
	// LiveOut[i] is the set live immediately after instruction i.
	LiveOut [][]bool
	// NextUse[i] gives the next instruction index at which each vreg is used
	// after instruction i (-1 if none), on the worst-case CFG path.
	NextUse [][]int
}

// ComputeLiveness analyzes dataflow over the CFG.
func ComputeLiveness(f *ir.Function, numVRegs int) *LiveRanges {
	type blockRef struct {
		start, end int
	}
	blocks := make([]blockRef, len(f.Blocks))
	blockIdx := map[string]int{}
	total := 0
	for bi, b := range f.Blocks {
		blockIdx[b.Name] = bi
		blocks[bi] = blockRef{start: total, end: total + len(b.Instrs)}
		total += len(b.Instrs)
	}

	lr := &LiveRanges{
		NumVRegs:   numVRegs,
		LiveAtExit: make([]bool, numVRegs),
		LiveIn:     make([][]bool, total),
		LiveOut:    make([][]bool, total),
		NextUse:    make([][]int, total),
	}
	for i := range lr.LiveIn {
		lr.LiveIn[i] = make([]bool, numVRegs)
		lr.LiveOut[i] = make([]bool, numVRegs)
	}

	set := func(dst []bool, src []bool) { copy(dst, src) }

	// live[b] = live-in of block b in block-liveness space.
	liveIn := make([][]bool, len(f.Blocks))
	liveOut := make([][]bool, len(f.Blocks))
	for bi := range f.Blocks {
		liveIn[bi] = make([]bool, numVRegs)
		liveOut[bi] = make([]bool, numVRegs)
	}

	changed := true
	for changed {
		changed = false
		for bi := len(f.Blocks) - 1; bi >= 0; bi-- {
			b := f.Blocks[bi]
			out := make([]bool, numVRegs)
			for _, s := range b.Succs {
				for r := 0; r < numVRegs; r++ {
					if liveIn[blockIdx[s]][r] {
						out[r] = true
					}
				}
			}
			cur := append([]bool(nil), out...)
			for ii := len(b.Instrs) - 1; ii >= 0; ii-- {
				ins := b.Instrs[ii]
				if ins.Dst.IsReg {
					cur[ins.Dst.Reg] = false
				}
				for _, u := range irUses(ins) {
					if u.IsReg {
						cur[u.Reg] = true
					}
				}
				set(lr.LiveIn[blocks[bi].start+ii], cur)
			}
			copy(lr.LiveOut[blocks[bi].end-1], out)
			if !boolEq(liveOut[bi], out) {
				copy(liveOut[bi], out)
				changed = true
			}
			if !boolEq(liveIn[bi], cur) {
				copy(liveIn[bi], cur)
				changed = true
			}
		}
	}

	// Fill per-instruction liveOut from the local scans by redoing each block
	// with the fixed-point block live-in/live-out values.
	for bi, b := range f.Blocks {
		cur := make([]bool, numVRegs)
		for _, s := range b.Succs {
			for r := 0; r < numVRegs; r++ {
				if liveIn[blockIdx[s]][r] {
					cur[r] = true
				}
			}
		}
		for ii := len(b.Instrs) - 1; ii >= 0; ii-- {
			idx := blocks[bi].start + ii
			copy(lr.LiveOut[idx], cur)
			ins := b.Instrs[ii]
			if ins.Dst.IsReg {
				cur[ins.Dst.Reg] = false
			}
			for _, u := range irUses(ins) {
				if u.IsReg {
					cur[u.Reg] = true
				}
			}
			copy(lr.LiveIn[idx], cur)
		}
	}

	// Worst-case next use: a register used along any path after point i is
	// considered live there, so the allocator must keep every such value safe.
	// We compute the nearest upcoming use reachable via the CFG using a reverse
	// fixed point over block ends.
	nextUseAt := make([][]int, total)
	for i := range nextUseAt {
		nextUseAt[i] = make([]int, numVRegs)
		for r := range nextUseAt[i] {
			nextUseAt[i][r] = -1
		}
	}
	blockEnd := make([][]int, len(f.Blocks))
	entryNext := make([][]int, len(f.Blocks))
	for bi := range entryNext {
		entryNext[bi] = make([]int, numVRegs)
		for r := range entryNext[bi] {
			entryNext[bi][r] = -1
		}
	}
	changed = true
	for changed {
		changed = false
		for bi := len(f.Blocks) - 1; bi >= 0; bi-- {
			b := f.Blocks[bi]
			end := make([]int, numVRegs)
			for r := range end {
				end[r] = -1
			}
			for _, s := range b.Succs {
				sbi := blockIdx[s]
				for r := 0; r < numVRegs; r++ {
					// Only values already live at the successor entry can
					// provide an incoming next-use; otherwise an instruction
					// later in the successor could look like a use of an
					// undefined value at this block boundary.
					if !lr.LiveIn[blocks[sbi].start][r] {
						continue
					}
					v := entryNext[sbi][r]
					if v >= 0 && (end[r] < 0 || v < end[r]) {
						end[r] = v
					}
				}
			}
			cur := append([]int(nil), end...)
			var head []int
			for ii := len(b.Instrs) - 1; ii >= 0; ii-- {
				idx := blocks[bi].start + ii
				copy(nextUseAt[idx], cur)
				if ii == 0 {
					head = append([]int(nil), cur...)
				}
				ins := b.Instrs[ii]
				for _, u := range irUses(ins) {
					if u.IsReg {
						cur[u.Reg] = idx
					}
				}
				if ins.Dst.IsReg {
					cur[ins.Dst.Reg] = -1
				}
			}
			if !intEq(blockEnd[bi], end) {
				blockEnd[bi] = end
				changed = true
			}
			if !intEq(entryNext[bi], head) {
				entryNext[bi] = head
				changed = true
			}
		}
	}
	lr.NextUse = nextUseAt

	idx := 0
	for _, b := range f.Blocks {
		lastIdx := idx + len(b.Instrs) - 1
		if b.Instrs[len(b.Instrs)-1].Op == ir.OpRet {
			for r := 0; r < numVRegs; r++ {
				if lr.LiveIn[lastIdx][r] {
					lr.LiveAtExit[r] = true
				}
			}
		}
		idx += len(b.Instrs)
	}
	return lr
}

func irUses(ins *ir.Instr) []ir.Operand {
	switch ins.Op {
	case ir.OpConst:
		return nil
	case ir.OpCall:
		return ins.Args
	case ir.OpStore, ir.OpBrCond:
		return []ir.Operand{ins.Src1, ins.Src2}
	case ir.OpLoad, ir.OpRet:
		return []ir.Operand{ins.Src1}
	case ir.OpBr:
		return nil
	default:
		return []ir.Operand{ins.Src1, ins.Src2}
	}
}

func boolEq(a, b []bool) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intEq(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
