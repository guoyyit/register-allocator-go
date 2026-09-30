package ir

import (
	"strconv"
	"strings"
)

// String renders a program back to canonical IR text.
func (p *Program) String() string {
	var b strings.Builder
	for _, f := range p.Funcs {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// String renders a function in canonical textual form.
func (f *Func) String() string {
	var b strings.Builder
	b.WriteString("func ")
	b.WriteString(f.Name)
	b.WriteByte('(')
	for i, r := range f.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(regName(r))
	}
	b.WriteString("):\n")
	for _, blk := range f.Blocks {
		b.WriteString("  ")
		b.WriteString(blk.Name)
		b.WriteString(":\n")
		for _, ins := range blk.Inst {
			b.WriteString("    ")
			b.WriteString(ins.String())
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func regName(r Reg) string { return "v" + strconv.Itoa(int(r)) }

// String renders one instruction canonically.
func (i *Inst) String() string {
	switch i.Op {
	case OpNop:
		return "nop"
	case OpConst:
		return regName(i.Dst) + " = " + strconv.FormatInt(i.Imm, 10)
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpCmpLt, OpCmpLe, OpCmpEq, OpCmpNe:
		return regName(i.Dst) + " = " + i.Op.String() + " " + regName(i.Src1) + ", " + regName(i.Src2)
	case OpMove:
		return regName(i.Dst) + " = " + regName(i.Src1)
	case OpLoad:
		return regName(i.Dst) + " = " + memName(i.Src1, i.Imm)
	case OpStore:
		return "store " + regName(i.Src1) + ", " + memName(i.Dst, i.Imm)
	case OpCall:
		s := regName(i.Dst) + " = " + i.Func + "("
		for j, a := range i.Args {
			if j > 0 {
				s += ", "
			}
			s += regName(a)
		}
		return s + ")"
	case OpRet:
		if i.HasSrc1 {
			return "ret " + regName(i.Src1)
		}
		return "ret"
	case OpJmp:
		return "jmp " + i.Label
	case OpBr:
		return "br " + regName(i.Src1) + ", " + i.Label + ", " + i.Next
	default:
		return i.Op.String()
	}
}

func memName(base Reg, imm int64) string {
	s := "[" + regName(base)
	if imm > 0 {
		s += " + " + strconv.FormatInt(imm, 10)
	} else if imm < 0 {
		s += " - " + strconv.FormatInt(-imm, 10)
	}
	return s + "]"
}
