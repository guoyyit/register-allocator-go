package alloc

import (
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

const simpleProg = `
func main
entry:
	%1 = const 10
	%2 = const 32
	%3 = add %1, %2
	store 0, %3
	%4 = load 0
	ret %4
`

func TestSmokeSimple(t *testing.T) {
	p, err := ir.Parse(simpleProg)
	if err != nil {
		t.Fatal(err)
	}
	mp, _, err := Compile(p, Config{NumPhysical: 3, MemSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	is, err := ir.Interpret(p, 64)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := target.Interpret(mp, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !StatesEqual(is, ts) {
		t.Fatalf("states differ\nir=%v\ntarget=%v\nasm:\n%s", is, ts, mp.String())
	}
}
