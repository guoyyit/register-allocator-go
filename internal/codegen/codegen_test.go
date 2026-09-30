package codegen

import (
	"strings"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/alloc"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

const prog = `
func main
entry:
	%1 = const 40
	%2 = const 2
	store %1, %2
	%3 = load %1
	ret %3
`

func TestBuildAndEquivalence(t *testing.T) {
	res, err := Build(prog, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Spills != 0 {
		t.Fatalf("expected no spills, got %d", res.Stats.Spills)
	}
	is, err := ir.Interpret(res.IR, DefaultOptions().MemSize)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := target.Interpret(res.Target, DefaultOptions().MemSize)
	if err != nil {
		t.Fatal(err)
	}
	if !alloc.StatesEqual(is, ts) {
		t.Fatal("states differ")
	}
}

func TestBuildRejectsInvalid(t *testing.T) {
	if _, err := Build("func main\nb:\n\tret %9\n", DefaultOptions()); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestBuildDeterministic(t *testing.T) {
	a, err := Build(prog, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(prog, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if a.Target.String() != b.Target.String() {
		t.Fatal("nondeterministic build")
	}
	if !strings.Contains(a.Target.String(), ".physical 4") {
		t.Fatal("missing physical register header")
	}
}

func TestOutOfBoundsMemoryRejected(t *testing.T) {
	src := "func main\nb:\n\t%1 = const 9999\n\tstore %1, %1\n\tret %1\n"
	res, err := Build(src, Options{NumPhysical: 4, MemSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ir.Interpret(res.IR, 8); err == nil {
		t.Fatal("IR interpreter must reject out-of-bounds access")
	}
	if _, err := target.Interpret(res.Target, 8); err == nil {
		t.Fatal("target interpreter must reject out-of-bounds access")
	}
}
