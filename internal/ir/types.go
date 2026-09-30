package ir

import (
	"fmt"
)

// Op is an IR opcode.
type Op int

const (
	OpNop Op = iota
	OpConst
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpLoad
	OpStore
	OpMove
	OpCmpLt
	OpCmpLe
	OpCmpEq
	OpCmpNe
	OpCall
	OpRet
	OpJmp
	OpBr
)

// Pos points to a location in the IR source text.
type Pos struct {
	File   string
	Line   int
	Column int
}

// String renders a source position.
func (p Pos) String() string { return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column) }

// Error is a located IR error.
type Error struct {
	Pos Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// Reg is a virtual register id (vN).
type Reg int

// Block is a basic block. Its last instruction is always a terminator.
type Block struct {
	Name string
	Pos  Pos
	Inst []*Inst
}

// Func is an IR function.
type Func struct {
	Name   string
	Pos    Pos
	Params []Reg
	Blocks []*Block
	// blockNames maps block name -> index in Blocks, filled by Parse.
	blockNames map[string]int
}

// BlockIndex returns the index of the named block and whether it exists.
func (f *Func) BlockIndex(name string) (int, bool) {
	i, ok := f.blockNames[name]
	return i, ok
}

// Entry returns the entry block.
func (f *Func) Entry() *Block { return f.Blocks[0] }

// Program is a complete IR program.
type Program struct {
	Funcs []*Func
	// funcNames maps function name -> index in Funcs, filled by Parse.
	funcNames map[string]int
}

// FuncIndex returns the index of the named function and whether it exists.
func (p *Program) FuncIndex(name string) (int, bool) {
	i, ok := p.funcNames[name]
	return i, ok
}

// Inst is a single IR instruction.
// Meaning of fields per opcode:
//
//	Const: Dst = int64(Imm)
//	Add/Sub/Mul/Div/Mod/CmpLt/CmpLe/CmpEq/CmpNe: Dst = Src1 op Src2
//	Load:  Dst = Mem[Src1 + Imm]
//	Store: Mem[Dst + Imm] = Src1
//	Move:  Dst = Src1
//	Call:  Dst = call Func, arguments are Args
//	Ret:   return Src1 (zero value if Src1 unset)
//	Jmp:   jump Label
//	Br:    if Src1 != 0 jump Label else fall through to Next
type Inst struct {
	Op      Op
	Pos     Pos
	Dst     Reg
	Src1    Reg
	Src2    Reg
	Imm     int64
	Func    string
	Args    []Reg
	Label   string
	Next    string
	HasDst  bool
	HasSrc1 bool
	HasSrc2 bool
}

// HasResult reports whether an opcode defines Dst.
func (o Op) HasResult() bool {
	switch o {
	case OpConst, OpAdd, OpSub, OpMul, OpDiv, OpMod, OpLoad, OpMove,
		OpCmpLt, OpCmpLe, OpCmpEq, OpCmpNe, OpCall:
		return true
	default:
		return false
	}
}

// IsTerm reports whether an opcode terminates a block.
func (o Op) IsTerm() bool { return o == OpJmp || o == OpBr || o == OpRet }

// String renders an opcode mnemonic.
func (o Op) String() string {
	if s, ok := mnemonics[o]; ok {
		return s
	}
	return fmt.Sprintf("op%d", int(o))
}

var mnemonics = map[Op]string{
	OpNop:   "nop",
	OpConst: "const",
	OpAdd:   "add",
	OpSub:   "sub",
	OpMul:   "mul",
	OpDiv:   "div",
	OpMod:   "mod",
	OpLoad:  "load",
	OpStore: "store",
	OpMove:  "move",
	OpCmpLt: "cmplt",
	OpCmpLe: "cmple",
	OpCmpEq: "cmpeq",
	OpCmpNe: "cmpne",
	OpCall:  "call",
	OpRet:   "ret",
	OpJmp:   "jmp",
	OpBr:    "br",
}
