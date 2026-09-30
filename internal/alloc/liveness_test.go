package alloc

import (
	"testing"

	"github.com/guoyyit/register-allocator-go/internal/ir"
)

func TestLivenessLoop(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 0
    jmp loop
  loop:
    v2 = cmplt v1, v0
    br v2, body, done
  body:
    v3 = add v1, v0
    v1 = v3
    v4 = 1
    v0 = sub v0, v4
    jmp loop
  done:
    ret v1
`
	p, err := ir.Parse("loop.ir", src)
	if err != nil {
		t.Fatal(err)
	}
	f := p.Funcs[0]
	n := VRegCount(f)
	ls := Analyze(f, n)

	// v1 must be live on entry to loop (used in the comparison and defined in
	// the back-edge predecessor).
	loop, _ := f.BlockIndex("loop")
	if !ls.LiveInAt(loop, 0, 1) {
		t.Fatal("v1 should be live-in to loop block")
	}
	// v0 is a loop-carried value and must also be live-in.
	if !ls.LiveInAt(loop, 0, 0) {
		t.Fatal("v0 should be live-in to loop block")
	}

	res, err := New(4).Allocate(p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Funcs[0].MaxPressure < 2 {
		t.Fatalf("expected max pressure >= 2, got %d", res.Funcs[0].MaxPressure)
	}
}

func TestTooManyParams(t *testing.T) {
	src := `
func f(v0, v1, v2):
  entry:
    ret v0
func main():
  entry:
    v9 = 1
    ret v9
`
	p, err := ir.Parse("x.ir", src)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckTooManyParams(p, 2); err == nil {
		t.Fatal("expected too-many-parameters error for k=2")
	}
	if err := CheckTooManyParams(p, 3); err != nil {
		t.Fatalf("k=3 should accept 3 params: %v", err)
	}
}
