package ir

// Op identifies an IR instruction.
type Op int

const (
	OpInvalid Op = iota
	OpConst
	OpAdd
	OpSub
	OpMul
	OpLoad
	OpStore
	OpCmpLT
	OpBr
	OpBrCond
	OpCall
	OpRet
)

// Operand is either a virtual register reference or an immediate.
type Operand struct {
	Reg   int
	Imm   int64
	IsReg bool
}

// Instr is a single IR instruction.
type Instr struct {
	Op    Op
	Dst   Operand
	Src1  Operand
	Src2  Operand
	Func  string
	Args  []Operand
	Block string
	Line  int
}

// BasicBlock is a labeled sequence of instructions.
type BasicBlock struct {
	Name   string
	Instrs []*Instr
	Succs  []string
	Line   int
}

// Function is an IR function with a CFG.
type Function struct {
	Name   string
	Params []int
	Blocks []*BasicBlock
	Entry  string
}

// Program is a complete IR module.
type Program struct {
	Funcs []*Function
	Main  *Function
}

// State is the observable execution state.
type State struct {
	Regs   map[int]int64
	Memory []int64
	RetVal int64
	HasRet bool
}
