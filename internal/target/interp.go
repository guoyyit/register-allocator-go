package target

import "fmt"

// Interpret executes a compiled program.
func Interpret(mp *MProgram, memSize int) (*State, error) {
	if mp == nil || len(mp.Funcs) == 0 {
		return nil, fmt.Errorf("target: empty program")
	}
	if mp.NumPhysical < 2 {
		return nil, fmt.Errorf("target: need at least 2 physical registers, got %d", mp.NumPhysical)
	}

	byName := map[string]*MFunction{}
	var mainFn *MFunction
	for _, f := range mp.Funcs {
		byName[f.Name] = f
		if f.Name == "main" {
			mainFn = f
		}
	}
	if mainFn == nil {
		return nil, fmt.Errorf("target: no main function")
	}
	for _, f := range mp.Funcs {
		if err := resolveLabels(f); err != nil {
			return nil, err
		}
	}

	mem := make([]int64, memSize)
	m := &tmachine{prog: mp, byName: byName, memory: mem}
	mainFrame := m.newFrame(mainFn, nil)
	ret, err := m.run(mainFrame)
	if err != nil {
		return nil, err
	}

	out := &State{
		Regs:    append([]int64(nil), mainFrame.regs...),
		Memory:  append([]int64(nil), mem...),
		VRegs:   map[int]int64{},
		RetVal:  ret,
		HasRet:  true,
		Spills:  m.spills,
		Reloads: m.reloads,
	}
	for _, loc := range mainFn.FinalLocs {
		if loc.VReg < len(mainFn.LiveAtExit) && !mainFn.LiveAtExit[loc.VReg] {
			continue
		}
		var v int64
		if loc.InReg {
			v = mainFrame.regs[loc.Reg]
		} else {
			v = mainFrame.slots[loc.Slot]
		}
		out.VRegs[loc.VReg] = v
	}
	return out, nil
}

func resolveLabels(f *MFunction) error {
	labels := map[string]int{}
	for i, ins := range f.Instrs {
		if ins.Op == MOpLabel {
			if _, dup := labels[ins.Label]; dup {
				return fmt.Errorf("target %s: duplicate label %q", f.Name, ins.Label)
			}
			labels[ins.Label] = i
		}
	}
	for i, ins := range f.Instrs {
		if ins.Op == MOpB {
			t, ok := labels[ins.Label]
			if !ok {
				return fmt.Errorf("target %s: undefined label %q", f.Name, ins.Label)
			}
			f.Instrs[i].Target1 = t
		}
		if ins.Op == MOpBnez {
			t1, ok := labels[ins.Label]
			if !ok {
				return fmt.Errorf("target %s: undefined label %q", f.Name, ins.Label)
			}
			t2, ok := labels[ins.Func]
			if !ok {
				return fmt.Errorf("target %s: undefined label %q", f.Name, ins.Func)
			}
			f.Instrs[i].Target1 = t1
			f.Instrs[i].Target2 = t2
		}
	}
	return nil
}

type tframe struct {
	fn    *MFunction
	pc    int
	regs  []int64
	slots []int64
}

type tmachine struct {
	prog    *MProgram
	byName  map[string]*MFunction
	memory  []int64
	spills  int
	reloads int
}

func (m *tmachine) newFrame(f *MFunction, args []int64) *tframe {
	fr := &tframe{
		fn:    f,
		regs:  make([]int64, m.prog.NumPhysical),
		slots: make([]int64, f.StackSlots),
	}
	for i, a := range args {
		fr.regs[i] = a
	}
	return fr
}

func (m *tmachine) run(fr *tframe) (int64, error) {
	for {
		ins := fr.fn.Instrs[fr.pc]
		switch ins.Op {
		case MOpLi:
			fr.regs[ins.Rd] = ins.Imm
		case MOpMov, MOpMovResult:
			fr.regs[ins.Rd] = fr.regs[ins.Rs1]
		case MOpAdd:
			fr.regs[ins.Rd] = fr.regs[ins.Rs1] + fr.regs[ins.Rs2]
		case MOpSub:
			fr.regs[ins.Rd] = fr.regs[ins.Rs1] - fr.regs[ins.Rs2]
		case MOpMul:
			fr.regs[ins.Rd] = fr.regs[ins.Rs1] * fr.regs[ins.Rs2]
		case MOpCmpLT:
			if fr.regs[ins.Rs1] < fr.regs[ins.Rs2] {
				fr.regs[ins.Rd] = 1
			} else {
				fr.regs[ins.Rd] = 0
			}
		case MOpLoad:
			addr := fr.regs[ins.Rs1]
			if addr < 0 || int(addr) >= len(m.memory) {
				return 0, fmt.Errorf("target load out of bounds at line %d: %d", ins.Line, addr)
			}
			fr.regs[ins.Rd] = m.memory[addr]
		case MOpStore:
			addr := fr.regs[ins.Rs1]
			if addr < 0 || int(addr) >= len(m.memory) {
				return 0, fmt.Errorf("target store out of bounds at line %d: %d", ins.Line, addr)
			}
			m.memory[addr] = fr.regs[ins.Rs2]
		case MOpB:
			fr.pc = ins.Target1
			continue
		case MOpBnez:
			if fr.regs[ins.Rs1] != 0 {
				fr.pc = ins.Target1
			} else {
				fr.pc = ins.Target2
			}
			continue
		case MOpLabel:
		case MOpSpill:
			fr.slots[ins.Slot] = fr.regs[ins.Rd]
			m.spills++
		case MOpReload:
			fr.regs[ins.Rd] = fr.slots[ins.Slot]
			m.reloads++
		case MOpCall:
			callee := m.byName[ins.Func]
			if callee == nil {
				return 0, fmt.Errorf("target call to undefined function %q", ins.Func)
			}
			args := make([]int64, callee.NumParams)
			for i := range args {
				args[i] = fr.regs[i]
			}
			cf := m.newFrame(callee, args)
			rv, err := m.run(cf)
			if err != nil {
				return 0, err
			}
			fr.regs[ins.Rd] = rv
		case MOpRet:
			return fr.regs[0], nil
		default:
			return 0, fmt.Errorf("target %s: illegal instruction at pc %d", fr.fn.Name, fr.pc)
		}
		fr.pc++
	}
}
