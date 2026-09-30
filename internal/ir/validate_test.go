package ir

import "testing"

func TestRejectInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown instruction": "func main\nb:\n\t%1 = bogus 1\n\tret %1\n",
		"missing destination": "func main\nb:\n\tconst 5\n\tret %1\n",
		"use undefined":       "func main\nb:\n\tret %1\n",
		"use before def":      "func main\nb:\n\t%1 = add %2, 1\n\tret %1\n",
		"bad branch target":   "func main\nb:\n\tbr nowhere\n",
		"block no terminator": "func main\nb:\n\t%1 = const 1\n",
		"terminator not last": "func main\nb:\n\tbr b\n\t%1 = const 1\n",
		"unreachable block":   "func main\na:\n\tbr a\nb:\n\t%1 = const 1\n\tbr b\n",
		"undefined call":      "func main\nb:\n\t%1 = call nope()\n\tret %1\n",
		"arg count mismatch":  "func f\nparam %1\nb:\n\tret %1\nfunc main\nb:\n\t%1 = call f(1, 2)\n\tret %1\n",
		"duplicate block":     "func main\nb:\n\tbr b\nb:\n\tret %1\n",
		"nondominated def":    "func main\na:\n\t%0 = const 1\n\tbrcond %0, b, c\nb:\n\t%1 = const 1\n\tbr d\nc:\n\tbr d\nd:\n\tret %1\n",
		"missing ret":         "func main\nb:\n\t%1 = const 1\n\tbr b\n",
		"instruction outside": "%1 = const 1\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			p, perr := Parse(src)
			var err error
			if perr == nil {
				err = Validate(p)
			} else {
				err = perr
			}
			if err == nil {
				t.Fatalf("expected rejection for %s", name)
			}
			if le, ok := err.(*Error); ok && le.Line == 0 && name != "empty program" {
				t.Fatalf("error for %s lacks source location: %v", name, err)
			}
		})
	}
}

func TestRejectEmptyAndMain(t *testing.T) {
	if _, err := Parse(""); err == nil {
		t.Fatal("empty input must be rejected")
	}
	src := "func f\nparam %1\nb:\n\tret %1\n"
	p, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Interpret(p, 8); err == nil {
		t.Fatal("program without main must be rejected")
	}
}

func TestErrorLocatable(t *testing.T) {
	src := "func main\nb:\n\t%1 = const 1\n\t%2 = add %9, %1\n\tret %2\n"
	_, err := Parse(src)
	if err == nil {
		p, _ := Parse(src)
		err = Validate(p)
	}
	le, ok := err.(*Error)
	if !ok || le.Line != 4 {
		t.Fatalf("expected line 4 error, got %v", err)
	}
}
