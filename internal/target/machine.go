package target

import (
	"fmt"
)

// State is the observable machine state after execution.
type State struct {
	RetVal int64
	Memory []int64
}

type frame struct {
	fi    int
	bi    int
	regs  []int64
	slots []int64
}

// Machine executes target programs.
type Machine struct {
	numRegs int
	memSize int
}

// NewMachine creates a target machine with numRegs registers and memSize words.
func NewMachine(numRegs, memSize int) *Machine {
	return &Machine{numRegs: numRegs, memSize: memSize}
}

// Run executes entry with args and returns the final state.
func (m *Machine) Run(p *Program, entry string, args []int64) (*State, error) {
	if p.NumRegs != m.numRegs {
		return nil, fmt.Errorf("program requires %d registers, machine has %d", p.NumRegs, m.numRegs)
	}
	idx := -1
	for i, f := range p.Funcs {
		if f.Name == entry {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("entry function %q not found", entry)
	}
	if len(args) != p.Funcs[idx].Params {
		return nil, fmt.Errorf("entry %q expects %d args, got %d", entry, p.Funcs[idx].Params, len(args))
	}

	mem := make([]int64, m.memSize)
	blockOf := make([]map[string]int, len(p.Funcs))
	for fi, f := range p.Funcs {
		labels := map[string]int{}
		for bi, b := range f.Blocks {
			labels[b.Name] = bi
		}
		blockOf[fi] = labels
	}

	var stack []*frame
	depth := 0
	var runFrame func(fi int, callArgs []int64) (int64, error)
	runFrame = func(fi int, callArgs []int64) (int64, error) {
		depth++
		if depth > 100000 {
			return 0, fmt.Errorf("call depth exceeded")
		}
		defer func() { depth-- }()

		f := p.Funcs[fi]
		fr := &frame{fi: fi, regs: make([]int64, m.numRegs)}
		stack = append(stack, fr)
		defer func() { stack = stack[:len(stack)-1] }()
		for i, a := range callArgs {
			fr.regs[i] = a
		}

		for {
			b := f.Blocks[fr.bi]
			for ii := range b.Inst {
				ins := &b.Inst[ii]
				switch ins.Op {
				case OpNop:
				case OpLi:
					fr.regs[ins.Rd] = ins.Imm
				case OpAdd, OpSub, OpMul, OpDiv, OpMod,
					OpCmpLt, OpCmpLe, OpCmpEq, OpCmpNe:
					v, err := evalBinop(ins.Op, fr.regs[ins.Rs1], fr.regs[ins.Rs2])
					if err != nil {
						return 0, fmt.Errorf("target: %s: %w", f.Name, err)
					}
					fr.regs[ins.Rd] = v
				case OpMov:
					fr.regs[ins.Rd] = fr.regs[ins.Rs1]
				case OpLoad:
					addr, err := checkMem(fr.regs[ins.Rs1], ins.Imm, m.memSize)
					if err != nil {
						return 0, err
					}
					fr.regs[ins.Rd] = mem[addr]
				case OpStore:
					addr, err := checkMem(fr.regs[ins.Rd], ins.Imm, m.memSize)
					if err != nil {
						return 0, err
					}
					mem[addr] = fr.regs[ins.Rs1]
				case OpSpill:
					fr.growSlots(ins.Slot + 1)
					fr.slots[ins.Slot] = fr.regs[ins.Rs1]
				case OpReload:
					fr.growSlots(ins.Slot + 1)
					fr.regs[ins.Rd] = fr.slots[ins.Slot]
				case OpCall:
					cfi := -1
					for i, cf := range p.Funcs {
						if cf.Name == ins.Func {
							cfi = i
							break
						}
					}
					if cfi < 0 {
						return 0, fmt.Errorf("target: call to undefined function %q", ins.Func)
					}
					rv, err := runFrame(cfi, fr.regs[:p.Funcs[cfi].Params])
					if err != nil {
						return 0, err
					}
					fr.regs[0] = rv
				case OpRet:
					return fr.regs[0], nil
				case OpJmp:
				case OpBr:
				default:
					return 0, fmt.Errorf("target: unsupported opcode %s", ins.Op)
				}
			}
			term := b.Inst[len(b.Inst)-1]
			switch term.Op {
			case OpJmp:
				next, ok := blockOf[fi][term.Label]
				if !ok {
					return 0, fmt.Errorf("target: jump to undefined block %q", term.Label)
				}
				fr.bi = next
			case OpBr:
				next, ok := blockOf[fi][term.Next]
				if fr.regs[term.Rs1] != 0 {
					next, ok = blockOf[fi][term.Label]
				}
				if !ok {
					return 0, fmt.Errorf("target: branch to undefined block")
				}
				fr.bi = next
			default:
				return 0, fmt.Errorf("target: block %q in %q fell through", b.Name, f.Name)
			}
		}
	}

	ret, err := runFrame(idx, args)
	if err != nil {
		return nil, err
	}
	return &State{RetVal: ret, Memory: mem}, nil
}

func (f *frame) growSlots(n int) {
	if len(f.slots) < n {
		grown := make([]int64, n)
		copy(grown, f.slots)
		f.slots = grown
	}
}

func checkMem(base int64, off int64, size int) (int, error) {
	addr := base + off
	if addr < 0 || addr >= int64(size) {
		return 0, fmt.Errorf("target: memory access out of bounds: address %d", addr)
	}
	return int(addr), nil
}

func evalBinop(op Op, a, b int64) (int64, error) {
	switch op {
	case OpAdd:
		return a + b, nil
	case OpSub:
		return a - b, nil
	case OpMul:
		return a * b, nil
	case OpDiv:
		if b == 0 {
			return 0, fmt.Errorf("integer divide by zero")
		}
		if a == -1<<63 && b == -1 {
			return a, nil
		}
		return a / b, nil
	case OpMod:
		if b == 0 {
			return 0, fmt.Errorf("integer modulo by zero")
		}
		if a == -1<<63 && b == -1 {
			return 0, nil
		}
		return a % b, nil
	case OpCmpLt:
		return b2i(a < b), nil
	case OpCmpLe:
		return b2i(a <= b), nil
	case OpCmpEq:
		return b2i(a == b), nil
	case OpCmpNe:
		return b2i(a != b), nil
	}
	return 0, fmt.Errorf("not a binary op: %s", op)
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
