package target

import (
	"fmt"
	"strings"
)

func opName(op MOp) string {
	names := map[MOp]string{
		MOpLi:        "li",
		MOpMov:       "mov",
		MOpAdd:       "add",
		MOpSub:       "sub",
		MOpMul:       "mul",
		MOpLoad:      "load",
		MOpStore:     "store",
		MOpCmpLT:     "cmplt",
		MOpB:         "b",
		MOpBnez:      "bnez",
		MOpLabel:     "label",
		MOpCall:      "call",
		MOpRet:       "ret",
		MOpSpill:     "spill",
		MOpReload:    "reload",
		MOpMovResult: "movresult",
	}
	if n, ok := names[op]; ok {
		return n
	}
	return fmt.Sprintf("op%d", int(op))
}

// String renders a compiled program as deterministic assembly text.
func (mp *MProgram) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, ".physical %d\n", mp.NumPhysical)
	for _, f := range mp.Funcs {
		fmt.Fprintf(&b, "func %s params=%d slots=%d\n", f.Name, f.NumParams, f.StackSlots)
		for _, ins := range f.Instrs {
			fmt.Fprintf(&b, "\t%s\n", ins.String())
		}
	}
	return b.String()
}

func (ins MInstr) String() string {
	r := func(x int) string { return fmt.Sprintf("r%d", x) }
	switch ins.Op {
	case MOpLi:
		return fmt.Sprintf("li %s, %d", r(ins.Rd), ins.Imm)
	case MOpMov, MOpMovResult:
		return fmt.Sprintf("%s %s, %s", opName(ins.Op), r(ins.Rd), r(ins.Rs1))
	case MOpAdd, MOpSub, MOpMul, MOpCmpLT:
		return fmt.Sprintf("%s %s, %s, %s", opName(ins.Op), r(ins.Rd), r(ins.Rs1), r(ins.Rs2))
	case MOpLoad:
		return fmt.Sprintf("load %s, %s", r(ins.Rd), r(ins.Rs1))
	case MOpStore:
		return fmt.Sprintf("store %s, %s", r(ins.Rs1), r(ins.Rs2))
	case MOpB:
		return fmt.Sprintf("b %s", ins.Label)
	case MOpBnez:
		return fmt.Sprintf("bnez %s, %s, %s", r(ins.Rs1), ins.Label, ins.Func)
	case MOpLabel:
		return fmt.Sprintf("%s:", ins.Label)
	case MOpCall:
		return fmt.Sprintf("call %s -> %s", ins.Func, r(ins.Rd))
	case MOpRet:
		return fmt.Sprintf("ret %s", r(ins.Rs1))
	case MOpSpill:
		return fmt.Sprintf("spill %s, [s%d]", r(ins.Rd), ins.Slot)
	case MOpReload:
		return fmt.Sprintf("reload %s, [s%d]", r(ins.Rd), ins.Slot)
	default:
		return opName(ins.Op)
	}
}
