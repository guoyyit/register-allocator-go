package interp

import (
	"fmt"

	"github.com/guoyyit/register-allocator-go/internal/ir"
)

// State is the observable machine state after execution.
type State struct {
	RetVal int64
	Memory []int64
}

// Machine directly interprets an IR program.
type Machine struct {
	memSize int
}

// NewMachine creates an interpreter with memSize words of shared memory.
func NewMachine(memSize int) *Machine {
	return &Machine{memSize: memSize}
}

// Run executes entry with args over the program and returns the final state.
func (m *Machine) Run(p *ir.Program, entry string, args []int64) (*State, error) {
	idx, ok := p.FuncIndex(entry)
	if !ok {
		return nil, fmt.Errorf("entry function %q not found", entry)
	}
	fn := p.Funcs[idx]
	if len(args) != len(fn.Params) {
		return nil, fmt.Errorf("entry %q expects %d args, got %d", entry, len(fn.Params), len(args))
	}
	if err := ir.Validate(p); err != nil {
		return nil, err
	}

	mem := make([]int64, m.memSize)
	frames := 0
	var call func(fi int, callArgs []int64) (int64, error)
	call = func(fi int, callArgs []int64) (int64, error) {
		frames++
		if frames > 100000 {
			return 0, fmt.Errorf("call depth exceeded")
		}
		defer func() { frames-- }()

		f := p.Funcs[fi]
		n := vregCount(f)
		regs := make([]int64, n)
		for i, r := range f.Params {
			if int(r) >= n {
				return 0, fmt.Errorf("internal: parameter v%d out of range", r)
			}
			regs[r] = callArgs[i]
		}

		bi := 0
		for {
			b := f.Blocks[bi]
			for _, ins := range b.Inst {
				switch ins.Op {
				case ir.OpNop:
				case ir.OpConst:
					regs[ins.Dst] = ins.Imm
				case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod,
					ir.OpCmpLt, ir.OpCmpLe, ir.OpCmpEq, ir.OpCmpNe:
					a, c := regs[ins.Src1], regs[ins.Src2]
					v, err := evalBinop(ins.Op, a, c, ins.Pos)
					if err != nil {
						return 0, err
					}
					regs[ins.Dst] = v
				case ir.OpMove:
					regs[ins.Dst] = regs[ins.Src1]
				case ir.OpLoad:
					addr, err := checkMem(regs[ins.Src1], ins.Imm, m.memSize, ins.Pos)
					if err != nil {
						return 0, err
					}
					regs[ins.Dst] = mem[addr]
				case ir.OpStore:
					addr, err := checkMem(regs[ins.Dst], ins.Imm, m.memSize, ins.Pos)
					if err != nil {
						return 0, err
					}
					mem[addr] = regs[ins.Src1]
				case ir.OpCall:
					cfi, _ := p.FuncIndex(ins.Func)
					cargs := make([]int64, len(ins.Args))
					for i, r := range ins.Args {
						cargs[i] = regs[r]
					}
					rv, err := call(cfi, cargs)
					if err != nil {
						return 0, err
					}
					regs[ins.Dst] = rv
				case ir.OpRet:
					if ins.HasSrc1 {
						return regs[ins.Src1], nil
					}
					return 0, nil
				case ir.OpJmp:
				case ir.OpBr:
				default:
					return 0, fmt.Errorf("internal: unsupported opcode %s", ins.Op)
				}
			}
			term := b.Inst[len(b.Inst)-1]
			switch term.Op {
			case ir.OpJmp:
				bi, _ = f.BlockIndex(term.Label)
			case ir.OpBr:
				cond := regs[term.Src1]
				if cond != 0 {
					bi, _ = f.BlockIndex(term.Label)
				} else {
					bi, _ = f.BlockIndex(term.Next)
				}
			default:
				return 0, fmt.Errorf("internal: block %q fell through without terminator", b.Name)
			}
		}
	}

	ret, err := call(idx, args)
	if err != nil {
		return nil, err
	}
	return &State{RetVal: ret, Memory: mem}, nil
}

func vregCount(f *ir.Func) int {
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
	return max + 1
}

func checkMem(base int64, off int64, size int, pos ir.Pos) (int, error) {
	addr := base + off
	if addr < 0 || addr >= int64(size) {
		return 0, &ir.Error{Pos: pos, Msg: fmt.Sprintf("memory access out of bounds: address %d (memory size %d)", addr, size)}
	}
	return int(addr), nil
}

func evalBinop(op ir.Op, a, b int64, pos ir.Pos) (int64, error) {
	switch op {
	case ir.OpAdd:
		return a + b, nil
	case ir.OpSub:
		return a - b, nil
	case ir.OpMul:
		return a * b, nil
	case ir.OpDiv:
		if b == 0 {
			return 0, &ir.Error{Pos: pos, Msg: "integer divide by zero"}
		}
		if a == -1<<63 && b == -1 {
			return a, nil // Go traps on this case; define overflow result as dividend.
		}
		return a / b, nil
	case ir.OpMod:
		if b == 0 {
			return 0, &ir.Error{Pos: pos, Msg: "integer modulo by zero"}
		}
		if a == -1<<63 && b == -1 {
			return 0, nil
		}
		return a % b, nil
	case ir.OpCmpLt:
		return b2i(a < b), nil
	case ir.OpCmpLe:
		return b2i(a <= b), nil
	case ir.OpCmpEq:
		return b2i(a == b), nil
	case ir.OpCmpNe:
		return b2i(a != b), nil
	}
	return 0, fmt.Errorf("internal: not a binary op: %s", op)
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
