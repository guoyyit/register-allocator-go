package alloc

import (
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// Stats counts allocator-generated memory traffic.
type Stats struct {
	Spills  int
	Reloads int
}

// Config fixes the machine model used by allocation.
type Config struct {
	NumPhysical int
	MemSize     int
}

// Compile lowers an IR program to deterministic target code.
func Compile(p *ir.Program, cfg Config) (*target.MProgram, Stats, error) {
	if p == nil {
		return nil, Stats{}, &ir.Error{Msg: "nil program"}
	}
	if cfg.NumPhysical < 3 {
		return nil, Stats{}, &ir.Error{Msg: "allocator requires at least 3 physical registers"}
	}
	if err := ir.Validate(p); err != nil {
		return nil, Stats{}, err
	}

	mp := &target.MProgram{NumPhysical: cfg.NumPhysical}
	var total Stats
	for _, f := range p.Funcs {
		mf, st, err := compileFunc(f, cfg)
		if err != nil {
			return nil, Stats{}, err
		}
		mp.Funcs = append(mp.Funcs, mf)
		total.Spills += st.Spills
		total.Reloads += st.Reloads
	}
	return mp, total, nil
}

// StatesEqual checks observable equivalence of IR and target final states.
func StatesEqual(a *ir.State, b *target.State) bool {
	if a == nil || b == nil {
		return false
	}
	if a.RetVal != b.RetVal || a.HasRet != b.HasRet {
		return false
	}
	if len(a.Memory) != len(b.Memory) {
		return false
	}
	for i := range a.Memory {
		if a.Memory[i] != b.Memory[i] {
			return false
		}
	}
	for v, tv := range b.VRegs {
		vv, ok := a.Regs[v]
		if !ok || vv != tv {
			return false
		}
	}
	return true
}

func numVRegs(f *ir.Function) int {
	max := -1
	note := func(r int) {
		if r > max {
			max = r
		}
	}
	for _, r := range f.Params {
		note(r)
	}
	for _, b := range f.Blocks {
		for _, ins := range b.Instrs {
			if ins.Dst.IsReg {
				note(ins.Dst.Reg)
			}
			for _, u := range operandsOf(ins) {
				if u.IsReg {
					note(u.Reg)
				}
			}
		}
	}
	return max + 1
}

func operandsOf(ins *ir.Instr) []ir.Operand {
	switch ins.Op {
	case ir.OpConst:
		return nil
	case ir.OpCall:
		return ins.Args
	case ir.OpStore, ir.OpBrCond:
		return []ir.Operand{ins.Src1, ins.Src2}
	case ir.OpLoad, ir.OpRet:
		return []ir.Operand{ins.Src1}
	default:
		return []ir.Operand{ins.Src1, ins.Src2}
	}
}

func compileFunc(f *ir.Function, cfg Config) (*target.MFunction, Stats, error) {
	nv := numVRegs(f)
	lr := ComputeLiveness(f, nv)
	e := newEmitter(f, cfg, lr, nv)
	return e.run()
}
