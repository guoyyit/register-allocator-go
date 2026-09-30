package interp

import (
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/ir"
)

// Computes sum(0..n) via a loop, stores n into memory[0], and adds
// a callee invocation that doubles its argument.
const loopIR = `
func double(v0):
  entry:
    v1 = 2
    v2 = mul v0, v1
    ret v2

func main(v0):
  entry:
    v1 = 0
    v8 = 1
    store v0, [v1]
    jmp loop
  loop:
    v2 = 0
    v3 = cmplt v2, v0
    br v3, body, done
  body:
    v4 = add v1, v0
    v5 = 1
    v6 = sub v0, v5
    v1 = v4
    v0 = v6
    jmp loop
  done:
    v7 = double(v1)
    store v7, [v8]
    ret v7
`

func TestInterpretLoopAndCall(t *testing.T) {
	p, err := ir.Parse("loop.ir", loopIR)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := NewMachine(64)
	st, err := m.Run(p, "main", []int64{5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// sum 0..5 = 15; double -> 30
	if st.RetVal != 30 {
		t.Fatalf("ret = %d, want 30", st.RetVal)
	}
	if st.Memory[0] != 5 {
		t.Fatalf("mem[0] = %d, want 5", st.Memory[0])
	}
	if st.Memory[1] != 30 {
		t.Fatalf("mem[1] = %d, want 30", st.Memory[1])
	}
}

func TestInterpretTrap(t *testing.T) {
	src := `
func main():
  entry:
    v0 = 1
    v1 = 0
    v2 = div v0, v1
    ret v2
`
	p, err := ir.Parse("trap.ir", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := NewMachine(16).Run(p, "main", nil); err == nil {
		t.Fatal("expected divide-by-zero error")
	}
}
