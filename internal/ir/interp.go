package ir

import "fmt"

// Interpret executes a program starting at main with the given input memory.
func Interpret(p *Program, memSize int) (*State, error) {
	if p == nil || p.Main == nil {
		return nil, errorf(0, "no main function")
	}
	if memSize < 0 {
		return nil, errorf(0, "negative memory size")
	}
	if err := Validate(p); err != nil {
		return nil, err
	}

	m := newMachine(p, memSize)
	root := newFrame(p.Main, nil)
	mainRet, err := m.run(root)
	if err != nil {
		return nil, err
	}

	state := &State{
		Regs:   map[int]int64{},
		Memory: append([]int64(nil), m.memory...),
		RetVal: mainRet,
		HasRet: true,
	}
	for r, v := range root.regs {
		state.Regs[r] = v
	}
	return state, nil
}

type frame struct {
	fn    *Function
	regs  map[int]int64
	block *BasicBlock
	pc    int
}

func newFrame(f *Function, args []int64) *frame {
	fr := &frame{
		fn:    f,
		regs:  map[int]int64{},
		block: f.Blocks[0],
	}
	for i, r := range f.Params {
		fr.regs[r] = args[i]
	}
	return fr
}

type machine struct {
	prog   *Program
	memory []int64
}

func newMachine(p *Program, memSize int) *machine {
	return &machine{prog: p, memory: make([]int64, memSize)}
}

func (m *machine) run(fr *frame) (ret int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("interpreter panic: %v", r)
		}
	}()

	blockByName := map[string]*BasicBlock{}
	for _, b := range fr.fn.Blocks {
		blockByName[b.Name] = b
	}

	for {
		ins := fr.block.Instrs[fr.pc]
		switch ins.Op {
		case OpConst:
			fr.regs[ins.Dst.Reg] = ins.Src1.Imm
		case OpAdd:
			fr.regs[ins.Dst.Reg] = m.val(fr, ins.Src1) + m.val(fr, ins.Src2)
		case OpSub:
			fr.regs[ins.Dst.Reg] = m.val(fr, ins.Src1) - m.val(fr, ins.Src2)
		case OpMul:
			fr.regs[ins.Dst.Reg] = m.val(fr, ins.Src1) * m.val(fr, ins.Src2)
		case OpCmpLT:
			if m.val(fr, ins.Src1) < m.val(fr, ins.Src2) {
				fr.regs[ins.Dst.Reg] = 1
			} else {
				fr.regs[ins.Dst.Reg] = 0
			}
		case OpLoad:
			addr := m.val(fr, ins.Src1)
			if addr < 0 || int(addr) >= len(m.memory) {
				return 0, errorf(ins.Line, "load out of bounds: address %d", addr)
			}
			fr.regs[ins.Dst.Reg] = m.memory[addr]
		case OpStore:
			addr := m.val(fr, ins.Src1)
			if addr < 0 || int(addr) >= len(m.memory) {
				return 0, errorf(ins.Line, "store out of bounds: address %d", addr)
			}
			m.memory[addr] = m.val(fr, ins.Src2)
		case OpBr:
			fr.block = blockByName[ins.Block]
			fr.pc = 0
			continue
		case OpBrCond:
			if m.val(fr, ins.Src1) != 0 {
				fr.block = blockByName[ins.Block]
			} else {
				fr.block = blockByName[ins.Func]
			}
			fr.pc = 0
			continue
		case OpCall:
			callee := m.funcByName(ins.Func)
			args := make([]int64, len(ins.Args))
			for i, a := range ins.Args {
				args[i] = m.val(fr, a)
			}
			cf := newFrame(callee, args)
			rv, cerr := m.run(cf)
			if cerr != nil {
				return 0, cerr
			}
			fr.regs[ins.Dst.Reg] = rv
		case OpRet:
			return m.val(fr, ins.Src1), nil
		default:
			return 0, errorf(ins.Line, "unexpected op %v", ins.Op)
		}
		fr.pc++
	}
}

func (m *machine) funcByName(name string) *Function {
	for _, f := range m.prog.Funcs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func (m *machine) val(fr *frame, op Operand) int64 {
	if !op.IsReg {
		return op.Imm
	}
	v, ok := fr.regs[op.Reg]
	if !ok {
		panic(fmt.Sprintf("undefined register %% %d", op.Reg))
	}
	return v
}
