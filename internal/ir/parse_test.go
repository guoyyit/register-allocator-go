package ir

import (
	"errors"
	"strings"
	"testing"
)

const sampleIR = `
func main(v0):
  entry:
    v1 = 10
    v2 = add v0, v1
    store v2, [v0]
    v3 = [v0]
    br v3, then, done
  then:
    v4 = 4
    v5 = sub v3, v4
    jmp done
  done:
    v6 = 0
    ret v6
`

func TestParseRoundTrip(t *testing.T) {
	p, err := Parse("test.ir", sampleIR)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	out := p.String()
	p2, err := Parse("test.ir", out)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, out)
	}
	if err := Validate(p2); err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if p2.String() != out {
		t.Fatalf("canonical form not stable")
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		// instruction outside block
		"v1 = 1\n",
		// unknown opcode / bad move syntax
		"func main():\n entry:\n   v1 = frobnicate v2\n   ret\n",
		// missing terminator
		"func main():\n entry:\n   v1 = 1\n",
		// jump to missing block
		"func main():\n entry:\n   jmp nowhere\n",
		// use of undefined
		"func main():\n entry:\n   v1 = add v2, v3\n   ret v1\n",
		// call arity
		"func callee(v0):\n entry:\n   ret v0\nfunc main():\n entry:\n   v1 = callee()\n   ret v1\n",
	}
	for i, src := range bad {
		p, err := Parse("bad.ir", src)
		if err == nil {
			err = Validate(p)
		}
		if err == nil {
			t.Errorf("case %d: expected error, program accepted:\n%s", i, src)
			continue
		}
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("case %d: error is not located: %v", i, err)
			continue
		}
		if !strings.Contains(err.Error(), "bad.ir:") {
			t.Errorf("case %d: error lacks file location: %v", i, err)
		}
	}
}
