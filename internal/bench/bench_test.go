// Package bench contains regression tests that enforce register-allocation
// quality budgets over the benchmark suite in testdata/bench.
package bench

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/codegen"
	"github.com/guoyyit/register-allocator-go/internal/interp"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// budget caps the spill and reload instruction counts generated for each
// benchmark at a given physical-register count. The ceilings are enforced by
// the test suite so an allocator change that degrades allocation quality fails
// CI instead of silently shipping more stack traffic.
type budget struct {
	file                  string
	k                     int
	maxSpills, maxReloads int
}

// The numbers are deliberately tight: they match the observed output of the
// current allocator with a small headroom margin. Raising a ceiling requires
// consciously accepting worse code.
var budgets = []budget{
	{"sum_loop.ir", 2, 30, 30},
	{"sum_loop.ir", 4, 30, 30},
	{"sum_loop.ir", 8, 30, 30},
	{"fib.ir", 2, 30, 30},
	{"fib.ir", 4, 30, 30},
	{"fib.ir", 8, 30, 30},
	{"high_pressure.ir", 2, 80, 80},
	{"high_pressure.ir", 4, 80, 80},
	{"high_pressure.ir", 8, 80, 80},
	{"cross_call.ir", 2, 40, 40},
	{"cross_call.ir", 4, 40, 40},
	{"cross_call.ir", 8, 40, 40},
	{"branch_maze.ir", 2, 30, 40},
	{"branch_maze.ir", 4, 30, 40},
	{"branch_maze.ir", 8, 30, 40},
}

func loadProg(t *testing.T, name string) *ir.Program {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bench", name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ir.Parse(name, string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := ir.Validate(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSpillBudgets(t *testing.T) {
	for _, b := range budgets {
		p := loadProg(t, b.file)
		res, err := codegen.Generate(p, b.k)
		if err != nil {
			t.Fatalf("%s k=%d: %v", b.file, b.k, err)
		}
		if res.Stats.Spills > b.maxSpills {
			t.Errorf("%s k=%d: spills=%d exceeds budget %d", b.file, b.k, res.Stats.Spills, b.maxSpills)
		}
		if res.Stats.Reloads > b.maxReloads {
			t.Errorf("%s k=%d: reloads=%d exceeds budget %d", b.file, b.k, res.Stats.Reloads, b.maxReloads)
		}
		t.Logf("%s k=%d spills=%d reloads=%d", b.file, b.k, res.Stats.Spills, res.Stats.Reloads)
	}
}

// entryArgs maps each benchmark to its entry arguments for equivalence runs.
var entryArgs = map[string][]int64{
	"sum_loop.ir":      {10},
	"fib.ir":           {12},
	"high_pressure.ir": {},
	"cross_call.ir":    {3},
	"branch_maze.ir":   {9},
}

// TestEquivalence runs every benchmark both on the IR interpreter and on the
// generated target code at several register counts and requires identical
// return values and memory contents.
func TestEquivalence(t *testing.T) {
	const memSize = 4096
	for _, b := range budgets {
		p := loadProg(t, b.file)
		args := entryArgs[b.file]
		want, err := interp.NewMachine(memSize).Run(p, "main", args)
		if err != nil {
			t.Fatalf("%s ir: %v", b.file, err)
		}
		res, err := codegen.Generate(p, b.k)
		if err != nil {
			t.Fatalf("%s k=%d: %v", b.file, b.k, err)
		}
		got, err := target.NewMachine(b.k, memSize).Run(res.Program, "main", args)
		if err != nil {
			t.Fatalf("%s k=%d target: %v", b.file, b.k, err)
		}
		if got.RetVal != want.RetVal {
			t.Errorf("%s k=%d: ret=%d want %d", b.file, b.k, got.RetVal, want.RetVal)
		}
		if !reflect.DeepEqual(got.Memory, want.Memory) {
			t.Errorf("%s k=%d: memory differs", b.file, b.k)
		}
	}
}
