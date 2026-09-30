package ir

// Validate checks a parsed program for semantic validity.
func Validate(p *Program) error {
	if p == nil {
		return errorf(0, "nil program")
	}
	byName := map[string]*Function{}
	for _, f := range p.Funcs {
		if f.Name == "" {
			return errorf(0, "anonymous function")
		}
		if _, dup := byName[f.Name]; dup {
			return errorf(0, "duplicate function %q", f.Name)
		}
		byName[f.Name] = f
	}
	for _, f := range p.Funcs {
		if err := validateFunc(f, byName); err != nil {
			return err
		}
	}
	return nil
}

type defSite struct {
	block int
	index int
}

func validateFunc(f *Function, funcs map[string]*Function) error {
	if len(f.Blocks) == 0 {
		return errorf(0, "function %q has no blocks", f.Name)
	}

	idx := map[string]int{}
	for i, b := range f.Blocks {
		if _, dup := idx[b.Name]; dup {
			return errorf(b.Line, "duplicate block label %q in %q", b.Name, f.Name)
		}
		idx[b.Name] = i
	}
	entry, ok := idx[f.Entry]
	if !ok {
		return errorf(0, "function %q missing entry block", f.Name)
	}

	paramSet := map[int]bool{}
	for _, r := range f.Params {
		if paramSet[r] {
			return errorf(f.Blocks[entry].Line, "duplicate param %%%d in %q", r, f.Name)
		}
		paramSet[r] = true
	}

	defs := map[int]defSite{}
	for _, r := range f.Params {
		defs[r] = defSite{block: entry, index: -1}
	}

	for bi, b := range f.Blocks {
		if len(b.Instrs) == 0 {
			return errorf(b.Line, "block %q is empty", b.Name)
		}
		for ii, ins := range b.Instrs {
			last := ii == len(b.Instrs)-1
			term := ins.Op == OpBr || ins.Op == OpBrCond || ins.Op == OpRet
			if last != term {
				if term {
					return errorf(ins.Line, "terminator must be the last instruction of block %q", b.Name)
				}
				return errorf(b.Instrs[len(b.Instrs)-1].Line, "block %q must end with br/brcond/ret", b.Name)
			}
			if ins.Dst.IsReg {
				if paramSet[ins.Dst.Reg] {
					return errorf(ins.Line, "cannot redefine parameter %%%d", ins.Dst.Reg)
				}
				if _, dup := defs[ins.Dst.Reg]; dup {
					return errorf(ins.Line, "register %%%d is defined more than once", ins.Dst.Reg)
				}
				defs[ins.Dst.Reg] = defSite{block: bi, index: ii}
			}
			switch ins.Op {
			case OpBr:
				if _, ok := idx[ins.Block]; !ok {
					return errorf(ins.Line, "br to undefined block %q", ins.Block)
				}
				addSucc(b, ins.Block)
			case OpBrCond:
				if _, ok := idx[ins.Block]; !ok {
					return errorf(ins.Line, "brcond to undefined block %q", ins.Block)
				}
				if _, ok := idx[ins.Func]; !ok {
					return errorf(ins.Line, "brcond to undefined block %q", ins.Func)
				}
				addSucc(b, ins.Block)
				addSucc(b, ins.Func)
			case OpCall:
				callee, ok := funcs[ins.Func]
				if !ok {
					return errorf(ins.Line, "call to undefined function %q", ins.Func)
				}
				if ins.Func == f.Name {
					return errorf(ins.Line, "recursive calls are not supported")
				}
				if len(ins.Args) != len(callee.Params) {
					return errorf(ins.Line, "call %s expects %d args, got %d", ins.Func, len(callee.Params), len(ins.Args))
				}
			}
		}
	}

	reachable := reachableBlocks(f, entry, idx)
	if len(reachable) != len(f.Blocks) {
		for i, b := range f.Blocks {
			if !reachable[i] {
				return errorf(b.Line, "block %q is unreachable from entry", b.Name)
			}
		}
	}
	dom := dominators(f, entry, idx, reachable)

	hasRet := false
	for bi, b := range f.Blocks {
		for ii, ins := range b.Instrs {
			if ins.Op == OpRet {
				hasRet = true
			}
			for _, use := range usesOf(ins) {
				if !use.IsReg {
					continue
				}
				site, ok := defs[use.Reg]
				if !ok {
					return errorf(ins.Line, "use of undefined register %%%d", use.Reg)
				}
				if site.block == bi {
					if site.index >= ii {
						return errorf(ins.Line, "register %%%d used before its definition", use.Reg)
					}
				} else if !dom[bi][site.block] {
					return errorf(ins.Line, "use of %%%d not dominated by its definition", use.Reg)
				}
			}
		}
	}
	if !hasRet {
		return errorf(f.Blocks[entry].Line, "function %q has no ret instruction", f.Name)
	}
	return nil
}

func addSucc(b *BasicBlock, name string) {
	for _, s := range b.Succs {
		if s == name {
			return
		}
	}
	b.Succs = append(b.Succs, name)
}

func usesOf(ins *Instr) []Operand {
	switch ins.Op {
	case OpConst:
		return nil
	case OpCall:
		return ins.Args
	case OpStore, OpBrCond:
		return []Operand{ins.Src1, ins.Src2}
	case OpLoad, OpRet, OpBr:
		if ins.Src1.IsReg {
			return []Operand{ins.Src1}
		}
		return nil
	default:
		return []Operand{ins.Src1, ins.Src2}
	}
}

func reachableBlocks(f *Function, entry int, idx map[string]int) []bool {
	seen := make([]bool, len(f.Blocks))
	stack := []int{entry}
	seen[entry] = true
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, s := range f.Blocks[cur].Succs {
			j := idx[s]
			if !seen[j] {
				seen[j] = true
				stack = append(stack, j)
			}
		}
	}
	return seen
}

func dominators(f *Function, entry int, idx map[string]int, live []bool) [][]bool {
	n := len(f.Blocks)
	all := make([]bool, n)
	for i := range all {
		all[i] = true
	}
	doms := make([][]bool, n)
	for i := range doms {
		d := make([]bool, n)
		if i != entry {
			copy(d, all)
		}
		doms[i] = d
	}
	doms[entry][entry] = true

	order := make([]int, 0, n)
	seen := make([]bool, n)
	var dfs func(int)
	dfs = func(b int) {
		seen[b] = true
		order = append(order, b)
		for _, s := range f.Blocks[b].Succs {
			j := idx[s]
			if live[j] && !seen[j] {
				dfs(j)
			}
		}
	}
	dfs(entry)

	changed := true
	for changed {
		changed = false
		for _, b := range order {
			if b == entry {
				continue
			}
			var preds []int
			for p, pb := range f.Blocks {
				if !live[p] {
					continue
				}
				for _, s := range pb.Succs {
					if idx[s] == b {
						preds = append(preds, p)
					}
				}
			}
			newSet := make([]bool, n)
			first := true
			for _, p := range preds {
				if !live[p] {
					continue
				}
				if first {
					copy(newSet, doms[p])
					first = false
				} else {
					for k := range newSet {
						newSet[k] = newSet[k] && doms[p][k]
					}
				}
			}
			newSet[b] = true
			if !boolSliceEqual(newSet, doms[b]) {
				doms[b] = newSet
				changed = true
			}
		}
	}
	return doms
}

func boolSliceEqual(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
