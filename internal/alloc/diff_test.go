package alloc

import (
	"strings"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

func diffCheck(t *testing.T, src string, regs []int) {
	t.Helper()
	p, err := ir.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	if err := ir.Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, n := range regs {
		mp, stats, err := Compile(p, Config{NumPhysical: n, MemSize: 256})
		if err != nil {
			t.Fatalf("compile regs=%d: %v", n, err)
		}
		is, err := ir.Interpret(p, 256)
		if err != nil {
			t.Fatalf("ir interp: %v", err)
		}
		ts, err := target.Interpret(mp, 256)
		if err != nil {
			t.Fatalf("target interp regs=%d: %v\n%s", n, err, mp.String())
		}
		if !StatesEqual(is, ts) {
			t.Fatalf("regs=%d states differ\nir=%v\ntarget=%v\nspills=%d reloads=%d\nasm:\n%s",
				n, is, ts, stats.Spills, stats.Reloads, mp.String())
		}
	}
}

const highPressure = `
func main
entry:
	%1 = const 1
	%2 = add %1, 1
	%3 = add %2, 1
	%4 = add %3, 1
	%5 = add %4, 1
	%6 = add %5, 1
	%7 = add %6, 1
	%8 = add %7, 1
	%9 = add %8, 1
	%10 = mul %1, %2
	%11 = add %10, %3
	%12 = add %11, %4
	%13 = add %12, %5
	%14 = add %13, %6
	%15 = add %14, %7
	%16 = add %15, %8
	%17 = add %16, %9
	store 0, %17
	ret %17
`

func TestDiffHighPressure(t *testing.T) {
	diffCheck(t, highPressure, []int{3, 4, 8})
}

const branchProg = `
func main
entry:
	%1 = const 3
	%2 = const 10
	%3 = cmplt %1, %2
	brcond %3, taken, skip
taken:
	%4 = add %1, %2
	br merge
skip:
	%5 = sub %2, %1
	br merge
merge:
	%6 = load 0
	store 1, %6
	ret %2
`

func TestDiffBranches(t *testing.T) {
	diffCheck(t, branchProg, []int{3, 4, 6})
}

const callProg = `
func add2
param %1
param %2
entry:
	%3 = add %1, %2
	ret %3

func main
entry:
	%10 = const 7
	%11 = const 8
	%12 = call add2(%10, %11)
	store 0, %12
	%13 = add %10, %11
	ret %13
`

func TestDiffCalls(t *testing.T) {
	diffCheck(t, callProg, []int{3, 4, 6})
}

func TestDeterminism(t *testing.T) {
	p, _ := ir.Parse(highPressure)
	var a, b string
	for i := 0; i < 2; i++ {
		mp, _, err := Compile(p, Config{NumPhysical: 3, MemSize: 256})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			a = mp.String()
		} else {
			b = mp.String()
		}
	}
	if a != b {
		t.Fatal("nondeterministic output")
	}
	if !strings.Contains(a, "spill") {
		t.Fatalf("expected spills under pressure:\n%s", a)
	}
}
