package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/guoyyit/register-allocator-go/internal/alloc"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

func main() {
	regs := flag.Int("regs", 4, "number of physical registers")
	mem := flag.Int("mem", 1024, "shared memory size in words")
	run := flag.Bool("run", false, "also execute both IR and target and compare states")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ra [flags] <file.ir>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	p, err := ir.Parse(string(data))
	if err != nil {
		fail(err)
	}
	mp, stats, err := alloc.Compile(p, alloc.Config{NumPhysical: *regs, MemSize: *mem})
	if err != nil {
		fail(err)
	}
	fmt.Print(mp.String())
	fmt.Fprintf(os.Stderr, "; spills=%d reloads=%d\n", stats.Spills, stats.Reloads)

	if *run {
		is, err := ir.Interpret(p, *mem)
		if err != nil {
			fail(err)
		}
		ts, err := target.Interpret(mp, *mem)
		if err != nil {
			fail(err)
		}
		if !alloc.StatesEqual(is, ts) {
			fmt.Fprintf(os.Stderr, "MISMATCH\nIR:     %v\nTarget: %v\n", is, ts)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "; ok ret=%d\n", is.RetVal)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
