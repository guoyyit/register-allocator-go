package target

import "testing"

func TestRunBasicAndMemory(t *testing.T) {
	p := &Program{
		NumRegs: 3,
		Funcs: []*Func{
			{
				Name:   "main",
				Params: 0,
				Blocks: []*Block{{
					Name: "entry",
					Inst: []Instruction{
						{Op: OpLi, Rd: 0, Imm: 2},
						{Op: OpLi, Rd: 1, Imm: 3},
						{Op: OpAdd, Rd: 2, Rs1: 0, Rs2: 1},
						{Op: OpLi, Rd: 0, Imm: 0},
						{Op: OpStore, Rs1: 2, Rd: 0, Imm: 0},
						{Op: OpLi, Rd: 0, Imm: 0},
						{Op: OpLoad, Rd: 0, Rs1: 0, Imm: 0},
						{Op: OpRet},
					},
				}},
			},
		},
	}
	st, err := NewMachine(3, 16).Run(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.RetVal != 5 || st.Memory[0] != 5 {
		t.Fatalf("got ret=%d mem0=%d, want 5/5", st.RetVal, st.Memory[0])
	}
}

func TestSpillReloadAcrossCall(t *testing.T) {
	// callee returns its argument; caller stores a live value in a slot across
	// the call and recovers it afterwards.
	p := &Program{
		NumRegs: 2,
		Funcs: []*Func{
			{
				Name: "callee", Params: 1,
				Blocks: []*Block{{Name: "e", Inst: []Instruction{{Op: OpRet}}}},
			},
			{
				Name: "main", Params: 0,
				Blocks: []*Block{{Name: "e", Inst: []Instruction{
					{Op: OpLi, Rd: 0, Imm: 41},
					{Op: OpSpill, Rs1: 0, Slot: 0},
					{Op: OpLi, Rd: 0, Imm: 7},
					{Op: OpCall, Func: "callee"},
					{Op: OpReload, Rd: 1, Slot: 0},
					{Op: OpAdd, Rd: 0, Rs1: 0, Rs2: 1},
					{Op: OpRet},
				}}},
			},
		},
	}
	st, err := NewMachine(2, 16).Run(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	// callee returns 7; spilled value 41 must survive.
	if st.RetVal != 48 {
		t.Fatalf("got %d, want 48", st.RetVal)
	}
}

func TestMemoryBounds(t *testing.T) {
	p := &Program{
		NumRegs: 1,
		Funcs: []*Func{{Name: "main", Params: 0, Blocks: []*Block{{Name: "e", Inst: []Instruction{
			{Op: OpLi, Rd: 0, Imm: 100},
			{Op: OpLoad, Rd: 0, Rs1: 0},
			{Op: OpRet},
		}}}}},
	}
	if _, err := NewMachine(1, 8).Run(p, "main", nil); err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}
