package target

import (
	"fmt"
	"strconv"
	"strings"
)

// Reg is a physical register id R0..R(K-1).
type Reg int

// Op is a target machine opcode.
type Op int

const (
	OpNop Op = iota
	OpLi
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpLoad
	OpStore
	OpMov
	OpCmpLt
	OpCmpLe
	OpCmpEq
	OpCmpNe
	OpSpill
	OpReload
	OpCall
	OpRet
	OpJmp
	OpBr
)

// Instruction is one target machine instruction.
type Instruction struct {
	Op    Op
	Rd    Reg
	Rs1   Reg
	Rs2   Reg
	Imm   int64
	Func  string
	Label string
	Next  string
	Slot  int
}

// Block is a target basic block.
type Block struct {
	Name string
	Inst []Instruction
}

// Func is a target function.
type Func struct {
	Name   string
	Params int
	Blocks []*Block
}

// Program is a complete target program.
type Program struct {
	NumRegs int
	Funcs   []*Func
}

// Stats summarizes generated code.
type Stats struct {
	Spills  int
	Reloads int
}

// String renders an opcode mnemonic.
func (o Op) String() string {
	if s, ok := mnemonics[o]; ok {
		return s
	}
	return fmt.Sprintf("op%d", int(o))
}

var mnemonics = map[Op]string{
	OpNop:    "nop",
	OpLi:     "li",
	OpAdd:    "add",
	OpSub:    "sub",
	OpMul:    "mul",
	OpDiv:    "div",
	OpMod:    "mod",
	OpLoad:   "load",
	OpStore:  "store",
	OpMov:    "mov",
	OpCmpLt:  "cmplt",
	OpCmpLe:  "cmple",
	OpCmpEq:  "cmpeq",
	OpCmpNe:  "cmpne",
	OpSpill:  "spill",
	OpReload: "reload",
	OpCall:   "call",
	OpRet:    "ret",
	OpJmp:    "jmp",
	OpBr:     "br",
}

// String renders a target program canonically.
func (p *Program) String() string {
	var b strings.Builder
	b.WriteString(".k ")
	b.WriteString(strconv.Itoa(p.NumRegs))
	b.WriteByte('\n')
	for _, f := range p.Funcs {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// String renders a target function canonically.
func (f *Func) String() string {
	var b strings.Builder
	b.WriteString("func ")
	b.WriteString(f.Name)
	b.WriteByte('(')
	b.WriteString(strconv.Itoa(f.Params))
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

func preg(r Reg) string { return "r" + strconv.Itoa(int(r)) }

// String renders one target instruction canonically.
func (i Instruction) String() string {
	switch i.Op {
	case OpNop:
		return "nop"
	case OpLi:
		return preg(i.Rd) + " = li " + strconv.FormatInt(i.Imm, 10)
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpCmpLt, OpCmpLe, OpCmpEq, OpCmpNe:
		return preg(i.Rd) + " = " + i.Op.String() + " " + preg(i.Rs1) + ", " + preg(i.Rs2)
	case OpMov:
		return preg(i.Rd) + " = " + preg(i.Rs1)
	case OpLoad:
		return preg(i.Rd) + " = load " + memName(i.Rs1, i.Imm)
	case OpStore:
		return "store " + preg(i.Rs1) + ", " + memName(i.Rd, i.Imm)
	case OpSpill:
		return "spill " + preg(i.Rs1) + ", slot" + strconv.Itoa(i.Slot)
	case OpReload:
		return preg(i.Rd) + " = reload slot" + strconv.Itoa(i.Slot)
	case OpCall:
		return "call " + i.Func
	case OpRet:
		return "ret"
	case OpJmp:
		return "jmp " + i.Label
	case OpBr:
		return "br " + preg(i.Rs1) + ", " + i.Label + ", " + i.Next
	default:
		return i.Op.String()
	}
}

func memName(base Reg, imm int64) string {
	return preg(base) + signedOff(imm)
}

func signedOff(imm int64) string {
	if imm > 0 {
		return " + " + strconv.FormatInt(imm, 10)
	}
	if imm < 0 {
		return " - " + strconv.FormatInt(-imm, 10)
	}
	return ""
}
