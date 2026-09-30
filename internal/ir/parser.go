package ir

import (
	"errors"
	"strconv"
	"strings"
)

var errBadOperand = errors.New("expected %<reg> or integer immediate")

// Parse parses IR text into a Program.
func Parse(text string) (*Program, error) {
	prog := &Program{}
	var fn *Function
	var blk *BasicBlock

	finish := func() {
		if fn != nil {
			prog.Funcs = append(prog.Funcs, fn)
			fn = nil
			blk = nil
		}
	}

	for lineNo, raw := range strings.Split(text, "\n") {
		line := lineNo + 1
		if i := strings.Index(raw, ";"); i >= 0 {
			raw = raw[:i]
		}
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		switch {
		case strings.HasPrefix(s, "func "):
			finish()
			name := strings.TrimSpace(s[len("func "):])
			if name == "" || strings.ContainsAny(name, " \t") {
				return nil, errorf(line, "invalid function header %q", s)
			}
			fn = &Function{Name: name}
			if name == "main" {
				if prog.Main != nil {
					return nil, errorf(line, "duplicate main function")
				}
				prog.Main = fn
			}
		case strings.HasPrefix(s, "param "):
			if fn == nil || blk != nil {
				return nil, errorf(line, "param must appear before any block")
			}
			op, err := parseOperand(strings.TrimSpace(s[len("param "):]))
			if err != nil {
				return nil, errorf(line, "invalid param: %v", err)
			}
			if !op.IsReg {
				return nil, errorf(line, "param must be a register")
			}
			fn.Params = append(fn.Params, op.Reg)
		case strings.HasSuffix(s, ":"):
			if fn == nil {
				return nil, errorf(line, "block outside function")
			}
			name := strings.TrimSpace(s[:len(s)-1])
			if name == "" {
				return nil, errorf(line, "empty block label")
			}
			blk = &BasicBlock{Name: name, Line: line}
			fn.Blocks = append(fn.Blocks, blk)
			if fn.Entry == "" {
				fn.Entry = name
			}
		default:
			if blk == nil {
				return nil, errorf(line, "instruction outside block: %q", s)
			}
			ins, err := parseInstr(s, line)
			if err != nil {
				return nil, err
			}
			blk.Instrs = append(blk.Instrs, ins)
		}
	}
	finish()

	if len(prog.Funcs) == 0 {
		return nil, errorf(0, "empty program")
	}
	return prog, nil
}

func parseInstr(s string, line int) (*Instr, error) {
	ins := &Instr{Line: line}

	if dst, rest, ok := cutDef(s); ok {
		op, err := parseOperand(dst)
		if err != nil || !op.IsReg {
			return nil, errorf(line, "invalid destination %q", dst)
		}
		ins.Dst = op
		s = rest
	}

	opName, args, _ := strings.Cut(s, " ")
	args = strings.TrimSpace(args)

	switch opName {
	case "const":
		ins.Op = OpConst
		v, err := strconv.ParseInt(args, 10, 64)
		if err != nil {
			return nil, errorf(line, "invalid const %q", args)
		}
		ins.Src1 = Operand{Imm: v}
	case "add", "sub", "mul", "cmplt":
		switch opName {
		case "add":
			ins.Op = OpAdd
		case "sub":
			ins.Op = OpSub
		case "mul":
			ins.Op = OpMul
		case "cmplt":
			ins.Op = OpCmpLT
		}
		a, b, err := parseTwo(args, line)
		if err != nil {
			return nil, err
		}
		ins.Src1, ins.Src2 = a, b
	case "load":
		ins.Op = OpLoad
		op, err := parseOperand(args)
		if err != nil {
			return nil, errorf(line, "invalid load operand: %v", err)
		}
		ins.Src1 = op
	case "store":
		ins.Op = OpStore
		a, b, err := parseTwo(args, line)
		if err != nil {
			return nil, err
		}
		ins.Src1, ins.Src2 = a, b
	case "br":
		ins.Op = OpBr
		ins.Block = args
		if ins.Block == "" {
			return nil, errorf(line, "br requires a target block")
		}
	case "brcond":
		ins.Op = OpBrCond
		parts := strings.Split(args, ",")
		if len(parts) != 3 {
			return nil, errorf(line, "brcond expects cond, label, label")
		}
		op, err := parseOperand(strings.TrimSpace(parts[0]))
		if err != nil || !op.IsReg {
			return nil, errorf(line, "brcond condition must be a register")
		}
		ins.Src1 = op
		ins.Block = strings.TrimSpace(parts[1])
		ins.Func = strings.TrimSpace(parts[2])
		if ins.Block == "" || ins.Func == "" {
			return nil, errorf(line, "brcond requires two target blocks")
		}
	case "call":
		ins.Op = OpCall
		name, argText, ok := strings.Cut(args, "(")
		ins.Func = strings.TrimSpace(name)
		if !ok || ins.Func == "" {
			return nil, errorf(line, "malformed call")
		}
		if !strings.HasSuffix(argText, ")") {
			return nil, errorf(line, "call missing closing parenthesis")
		}
		argText = strings.TrimSuffix(argText, ")")
		if strings.TrimSpace(argText) != "" {
			for _, a := range strings.Split(argText, ",") {
				op, err := parseOperand(strings.TrimSpace(a))
				if err != nil {
					return nil, errorf(line, "invalid call argument: %v", err)
				}
				ins.Args = append(ins.Args, op)
			}
		}
	case "ret":
		ins.Op = OpRet
		op, err := parseOperand(args)
		if err != nil {
			return nil, errorf(line, "invalid ret operand: %v", err)
		}
		ins.Src1 = op
	default:
		return nil, errorf(line, "unknown instruction %q", opName)
	}

	needsDst := ins.Op != OpBr && ins.Op != OpBrCond && ins.Op != OpRet && ins.Op != OpStore
	if needsDst && !ins.Dst.IsReg {
		return nil, errorf(line, "%s requires a destination register", opName)
	}
	if !needsDst && ins.Dst.IsReg {
		return nil, errorf(line, "%s must not define a register", opName)
	}
	return ins, nil
}

func cutDef(s string) (string, string, bool) {
	dst, rest, ok := strings.Cut(s, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(dst), strings.TrimSpace(rest), true
}

func parseTwo(args string, line int) (Operand, Operand, error) {
	parts := strings.Split(args, ",")
	if len(parts) != 2 {
		return Operand{}, Operand{}, errorf(line, "expected two operands separated by comma")
	}
	a, err := parseOperand(strings.TrimSpace(parts[0]))
	if err != nil {
		return Operand{}, Operand{}, errorf(line, "invalid operand: %v", err)
	}
	b, err := parseOperand(strings.TrimSpace(parts[1]))
	if err != nil {
		return Operand{}, Operand{}, errorf(line, "invalid operand: %v", err)
	}
	return a, b, nil
}

func parseOperand(s string) (Operand, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Operand{}, errBadOperand
	}
	if s[0] == '%' {
		v, err := strconv.Atoi(s[1:])
		if err != nil || v < 0 {
			return Operand{}, errBadOperand
		}
		return Operand{Reg: v, IsReg: true}, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return Operand{}, err
	}
	return Operand{Imm: v}, nil
}
