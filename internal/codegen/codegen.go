package codegen

import (
	"github.com/guoyyit/register-allocator-go/internal/alloc"
	"github.com/guoyyit/register-allocator-go/internal/ir"
	"github.com/guoyyit/register-allocator-go/internal/target"
)

// Options controls a backend run.
type Options struct {
	NumPhysical int
	MemSize     int
}

// Result bundles the artifacts of one backend run.
type Result struct {
	IR     *ir.Program
	Target *target.MProgram
	Stats  alloc.Stats
}

// Build parses, validates and compiles IR text.
func Build(text string, opts Options) (*Result, error) {
	p, err := ir.Parse(text)
	if err != nil {
		return nil, err
	}
	if err := ir.Validate(p); err != nil {
		return nil, err
	}
	cfg := alloc.Config{NumPhysical: opts.NumPhysical, MemSize: opts.MemSize}
	mp, stats, err := alloc.Compile(p, cfg)
	if err != nil {
		return nil, err
	}
	return &Result{IR: p, Target: mp, Stats: stats}, nil
}

// DefaultOptions is the deterministic machine model used by the benchmarks.
func DefaultOptions() Options {
	return Options{NumPhysical: 4, MemSize: 1024}
}
