package target

// MOp identifies a target machine instruction.
type MOp int

const (
	MOpInvalid MOp = iota
	MOpLi
	MOpMov
	MOpAdd
	MOpSub
	MOpMul
	MOpLoad
	MOpStore
	MOpCmpLT
	MOpB
	MOpBnez
	MOpLabel
	MOpCall
	MOpRet
	MOpSpill
	MOpReload
	MOpMovResult
)

// VLoc is the final home of a virtual register after a function finishes.
type VLoc struct {
	VReg  int
	InReg bool
	Reg   int
	Slot  int
}

// MInstr is one target instruction.
type MInstr struct {
	Op      MOp
	Rd      int
	Rs1     int
	Rs2     int
	Imm     int64
	Slot    int
	Func    string
	Label   string
	Target1 int
	Target2 int
	Line    int
}

// MFunction is a compiled function.
type MFunction struct {
	Name       string
	NumParams  int
	StackSlots int
	Instrs     []MInstr
	FinalLocs  []VLoc
	LiveAtExit []bool
}

// MProgram is a compiled module.
type MProgram struct {
	NumPhysical int
	Funcs       []*MFunction
}

// State is observable target-machine state.
type State struct {
	Regs    []int64
	Memory  []int64
	Stack   []int64
	VRegs   map[int]int64
	RetVal  int64
	HasRet  bool
	Spills  int
	Reloads int
}
