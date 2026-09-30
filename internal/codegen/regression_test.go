package codegen

import (
	"strings"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/interp"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

func runEquivalent(t *testing.T, src, entry string, k int, args []int64) {
	t.Helper()
	p, err := ir.Parse("case.ir", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ir.Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	res, err := Generate(p, k)
	if err != nil {
		t.Fatalf("generate k=%d: %v", k, err)
	}
	want, err := interp.NewMachine(2048).Run(p, entry, args)
	if err != nil {
		t.Fatalf("ir run: %v", err)
	}
	got, err := target.NewMachine(k, 2048).Run(res.Program, entry, args)
	if err != nil {
		t.Fatalf("target run k=%d: %v", k, err)
	}
	if got.RetVal != want.RetVal {
		t.Fatalf("k=%d ret=%d want %d", k, got.RetVal, want.RetVal)
	}
	for i := range want.Memory {
		if got.Memory[i] != want.Memory[i] {
			t.Fatalf("k=%d mem[%d]=%d want %d", k, i, got.Memory[i], want.Memory[i])
		}
	}
}

// Many live values survive across a call and must be preserved.
func TestLiveAcrossCall(t *testing.T) {
	src := `
func callee(v0):
  entry:
    v1 = 7
    v2 = add v0, v1
    ret v2

func main(v0):
  entry:
    v1 = add v0, v0
    v2 = add v1, v1
    v3 = add v2, v2
    v4 = add v3, v3
    v5 = add v4, v4
    v6 = add v5, v5
    v7 = callee(v0)
    v8 = add v7, v1
    v9 = add v8, v2
    v10 = add v9, v3
    v11 = add v10, v4
    v12 = add v11, v5
    v13 = add v12, v6
    ret v13
`
	for _, k := range []int{2, 3, 4, 8} {
		runEquivalent(t, src, "main", k, []int64{3})
	}
}

// Every physical register is occupied by an argument at the same time.
func TestAllRegsAreArgs(t *testing.T) {
	src := `
func sum4(v0, v1, v2, v3):
  entry:
    v4 = add v0, v1
    v5 = add v4, v2
    v6 = add v5, v3
    ret v6

func main():
  entry:
    v10 = 10
    v11 = 20
    v12 = 30
    v13 = 40
    v0 = sum4(v10, v11, v12, v13)
    ret v0
`
	for _, k := range []int{4, 5, 8} {
		runEquivalent(t, src, "main", k, nil)
	}
}

// Extreme pressure: far more simultaneously live values than registers.
func TestExtremePressure(t *testing.T) {
	var b strings.Builder
	b.WriteString("func main():\n  entry:\n")
	const n = 200
	for i := 0; i < n; i++ {
		b.WriteString("    v")
		b.WriteString(itoa(i))
		b.WriteString(" = ")
		b.WriteString(itoa(i + 1))
		b.WriteString("\n")
	}
	b.WriteString("    v900 = 0\n")
	for i := 0; i < n; i++ {
		b.WriteString("    v901 = add v900, v")
		b.WriteString(itoa(i))
		b.WriteString("\n    v900 = v901\n")
	}
	b.WriteString("    ret v900\n")
	for _, k := range []int{2, 3, 4} {
		runEquivalent(t, b.String(), "main", k, nil)
	}
}

// Determinism: identical input yields byte-identical target code repeatedly.
func TestDeterministicOutput(t *testing.T) {
	src := `
func g(v0):
  entry:
    v1 = 3
    v2 = mul v0, v1
    ret v2
func main(v0):
  entry:
    v1 = g(v0)
    br v1, yes, no
  yes:
    v2 = add v1, v0
    ret v2
  no:
    ret v1
`
	p, err := ir.Parse("d.ir", src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Generate(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		q, err := ir.Parse("d.ir", src)
		if err != nil {
			t.Fatal(err)
		}
		next, err := Generate(q, 3)
		if err != nil {
			t.Fatal(err)
		}
		if next.Program.String() != first.Program.String() {
			t.Fatalf("run %d produced different output", i)
		}
	}
}

// Invalid programs must be rejected, never lowered to broken target code.
func TestRejectsInvalid(t *testing.T) {
	cases := []string{
		// too many parameters for the calling convention
		"func f(v0, v1, v2, v3, v4):\n entry:\n   ret v0\nfunc main():\n entry:\n   v1 = 1\n   v9 = f(v1, v1, v1, v1, v1)\n   ret v9\n",
	}
	for i, src := range cases {
		p, err := ir.Parse("bad.ir", src)
		if err == nil {
			err = ir.Validate(p)
		}
		if err == nil {
			if _, gerr := Generate(p, 4); gerr == nil {
				t.Fatalf("case %d: invalid program accepted", i)
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
