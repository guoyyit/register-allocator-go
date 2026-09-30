package codegen

import (
	"fmt"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/interp"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

const eqIR = `
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

func TestEquivalence(t *testing.T) {
	for _, k := range []int{2, 3, 4, 8} {
		p, err := ir.Parse("eq.ir", eqIR)
		if err != nil {
			t.Fatalf("k=%d parse: %v", k, err)
		}
		res, err := Generate(p, k)
		if err != nil {
			t.Fatalf("k=%d generate: %v", k, err)
		}
		t.Logf("k=%d spills=%d reloads=%d", k, res.Stats.Spills, res.Stats.Reloads)

		want, err := interp.NewMachine(64).Run(p, "main", []int64{7})
		if err != nil {
			t.Fatalf("k=%d ir run: %v", k, err)
		}
		got, err := target.NewMachine(k, 64).Run(res.Program, "main", []int64{7})
		if err != nil {
			t.Fatalf("k=%d target run: %v\n%s", k, err, res.Program.String())
		}
		if got.RetVal != want.RetVal {
			t.Fatalf("k=%d ret got %d want %d", k, got.RetVal, want.RetVal)
		}
		for i := range want.Memory {
			if got.Memory[i] != want.Memory[i] {
				t.Fatalf("k=%d mem[%d] got %d want %d", k, i, got.Memory[i], want.Memory[i])
			}
		}
	}
}

func TestDeterminism(t *testing.T) {
	p, _ := ir.Parse("eq.ir", eqIR)
	a, err := Generate(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		p2, _ := ir.Parse("eq.ir", eqIR)
		b, err := Generate(p2, 3)
		if err != nil {
			t.Fatal(err)
		}
		if b.Program.String() != a.Program.String() {
			t.Fatalf("generation %d differs", i)
		}
	}
}

func TestHighPressure(t *testing.T) {
	var src string
	src += "func main():\n  entry:\n"
	const n = 40
	for i := 0; i < n; i++ {
		src += fmt.Sprintf("    v%d = %d\n", i, i+1)
	}
	src += "    v100 = 0\n"
	acc := "v0"
	for i := 1; i < n; i++ {
		src += fmt.Sprintf("    v100 = add %s, v%d\n", acc, i)
		acc = "v100"
	}
	src += "    v200 = 0\n    store v100, [v200]\n    ret v100\n"

	p, err := ir.Parse("hp.ir", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, k := range []int{2, 3, 4} {
		res, err := Generate(p, k)
		if err != nil {
			t.Fatalf("k=%d generate: %v", k, err)
		}
		want, err := interp.NewMachine(4096).Run(p, "main", nil)
		if err != nil {
			t.Fatalf("ir: %v", err)
		}
		got, err := target.NewMachine(k, 4096).Run(res.Program, "main", nil)
		if err != nil {
			t.Fatalf("k=%d target: %v\n%s", k, err, res.Program.String())
		}
		if got.RetVal != want.RetVal || got.Memory[0] != want.Memory[0] {
			t.Fatalf("k=%d mismatch: got (%d,%d) want (%d,%d)", k, got.RetVal, got.Memory[0], want.RetVal, want.Memory[0])
		}
	}
}
