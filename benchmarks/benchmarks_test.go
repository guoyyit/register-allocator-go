package benchmarks

import (
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/alloc"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

func TestSpillLimits(t *testing.T) {
	for _, lim := range Limits() {
		t.Run(lim.Name, func(t *testing.T) {
			src := Programs()[lim.Name]
			p, err := ir.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			cfg := alloc.Config{NumPhysical: lim.NumPhysical, MemSize: 1024}
			mp, stats, err := alloc.Compile(p, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Spills > lim.MaxSpills {
				t.Fatalf("%s: spill count %d exceeds enforced limit %d",
					lim.Name, stats.Spills, lim.MaxSpills)
			}
			is, err := ir.Interpret(p, 1024)
			if err != nil {
				t.Fatal(err)
			}
			ts, err := target.Interpret(mp, 1024)
			if err != nil {
				t.Fatal(err)
			}
			if !alloc.StatesEqual(is, ts) {
				t.Fatalf("%s: IR and target states differ", lim.Name)
			}
			if stats.Spills == 0 {
				t.Fatalf("%s: expected register pressure to force spills", lim.Name)
			}
			t.Logf("%s: spills=%d reloads=%d (limit %d)",
				lim.Name, stats.Spills, stats.Reloads, lim.MaxSpills)
		})
	}
}

func TestBenchmarkDeterminism(t *testing.T) {
	for name, src := range Programs() {
		p, err := ir.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		cfg := alloc.Config{NumPhysical: 4, MemSize: 1024}
		var first string
		for i := 0; i < 3; i++ {
			mp, _, err := alloc.Compile(p, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				first = mp.String()
			} else if mp.String() != first {
				t.Fatalf("%s: non-deterministic code generation", name)
			}
		}
	}
}
