package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/guoyyit/register-allocator-go/internal/codegen"
	"github.com/guoyyit/register-allocator-go/internal/interp"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	k := fs.Int("k", 4, "number of physical registers")
	mem := fs.Int("mem", 4096, "shared memory size in 64-bit words")
	entry := fs.String("entry", "main", "entry function")
	argsFlag := fs.String("args", "", "comma-separated int64 entry arguments")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		usage()
		os.Exit(2)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fail("read %s: %v", fs.Arg(0), err)
	}
	prog, err := ir.Parse(fs.Arg(0), string(data))
	if err != nil {
		fail("%v", err)
	}
	if err := ir.Validate(prog); err != nil {
		fail("%v", err)
	}

	var args []int64
	if strings.TrimSpace(*argsFlag) != "" {
		for _, a := range strings.Split(*argsFlag, ",") {
			v, err := strconv.ParseInt(strings.TrimSpace(a), 10, 64)
			if err != nil {
				fail("bad -args entry %q: %v", a, err)
			}
			args = append(args, v)
		}
	}

	switch cmd {
	case "compile":
		res, err := codegen.Generate(prog, *k)
		if err != nil {
			fail("%v", err)
		}
		fmt.Print(res.Program.String())
		fmt.Fprintf(os.Stderr, "# spills=%d reloads=%d\n", res.Stats.Spills, res.Stats.Reloads)
	case "run-ir":
		st, err := interp.NewMachine(*mem).Run(prog, *entry, args)
		if err != nil {
			fail("%v", err)
		}
		fmt.Printf("ret=%d\n", st.RetVal)
	case "run":
		res, err := codegen.Generate(prog, *k)
		if err != nil {
			fail("%v", err)
		}
		st, err := target.NewMachine(*k, *mem).Run(res.Program, *entry, args)
		if err != nil {
			fail("%v", err)
		}
		fmt.Printf("ret=%d spills=%d reloads=%d\n", st.RetVal, res.Stats.Spills, res.Stats.Reloads)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ra <compile|run|run-ir> [flags] file.ir")
	flag.PrintDefaults()
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ra: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}
