package ir

import (
	"fmt"
	"strconv"
	"strings"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tReg
	tInt
	tPunct
)

type token struct {
	kind tokKind
	text string
	col  int
	val  int64
}

type lineParser struct {
	file string
	line int
	src  string
	toks []token
	pos  int
}

var opByName = map[string]Op{}

func init() {
	for op, name := range mnemonics {
		opByName[name] = op
	}
}

// Parse parses a complete IR source file.
func Parse(filename, src string) (*Program, error) {
	prog := &Program{funcNames: map[string]int{}}
	var cur *Func
	var block *Block

	for lineNo, raw := range strings.Split(src, "\n") {
		toks, perr := tokenize(raw)
		if perr != nil {
			perr.Pos.File = filename
			perr.Pos.Line = lineNo + 1
			return nil, perr
		}
		if len(toks) == 0 {
			continue
		}
		p := &lineParser{file: filename, line: lineNo + 1, src: raw, toks: toks}

		last := toks[len(toks)-1]
		if last.kind == tPunct && last.text == ":" {
			if len(toks) >= 2 && toks[0].text == "func" {
				f, err := p.parseFuncHeader()
				if err != nil {
					return nil, err
				}
				if cur != nil && len(cur.Blocks) == 0 {
					return nil, &Error{Pos: cur.Pos, Msg: fmt.Sprintf("function %q has no blocks", cur.Name)}
				}
				if _, dup := prog.funcNames[f.Name]; dup {
					return nil, &Error{Pos: f.Pos, Msg: fmt.Sprintf("duplicate function %q", f.Name)}
				}
				prog.funcNames[f.Name] = len(prog.Funcs)
				prog.Funcs = append(prog.Funcs, f)
				cur = f
				block = nil
			} else {
				if cur == nil {
					return nil, &Error{Pos: p.posOf(0), Msg: "block label outside of function"}
				}
				b, err := p.parseLabel()
				if err != nil {
					return nil, err
				}
				if _, dup := cur.blockNames[b.Name]; dup {
					return nil, &Error{Pos: b.Pos, Msg: fmt.Sprintf("duplicate block %q in function %q", b.Name, cur.Name)}
				}
				cur.blockNames[b.Name] = len(cur.Blocks)
				cur.Blocks = append(cur.Blocks, b)
				block = b
			}
			continue
		}

		if cur == nil || block == nil {
			return nil, &Error{Pos: p.posOf(0), Msg: "instruction outside of block"}
		}
		ins, err := p.parseInst()
		if err != nil {
			return nil, err
		}
		block.Inst = append(block.Inst, ins)
	}
	if cur != nil && len(cur.Blocks) == 0 {
		return nil, &Error{Pos: cur.Pos, Msg: fmt.Sprintf("function %q has no blocks", cur.Name)}
	}
	if len(prog.Funcs) == 0 {
		return nil, &Error{Pos: Pos{File: filename, Line: 1, Column: 1}, Msg: "program contains no functions"}
	}
	return prog, nil
}

func tokenize(line string) ([]token, *Error) {
	var toks []token
	runes := []rune(line)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch {
		case c == '#' || c == ';' || (c == '/' && i+1 < len(runes) && runes[i+1] == '/'):
			return toks, nil
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case strings.ContainsRune(":,[]=+-()", c):
			toks = append(toks, token{kind: tPunct, text: string(c), col: i + 1})
			i++
		case c == 'v' || c == 'V':
			start := i
			i++
			for i < len(runes) && isDigit(runes[i]) {
				i++
			}
			if i == start+1 || (i < len(runes) && isIdentRune(runes[i])) {
				return nil, tokErr(line, i+1, "invalid virtual register")
			}
			n, err := strconv.ParseInt(string(runes[start+1:i]), 10, 64)
			if err != nil || n < 0 {
				return nil, tokErr(line, start+1, "invalid virtual register")
			}
			toks = append(toks, token{kind: tReg, text: string(runes[start:i]), col: start + 1, val: n})
		case isDigit(c):
			start := i
			for i < len(runes) && isDigit(runes[i]) {
				i++
			}
			n, err := strconv.ParseInt(string(runes[start:i]), 10, 64)
			if err != nil {
				return nil, tokErr(line, start+1, "integer literal out of range")
			}
			toks = append(toks, token{kind: tInt, text: string(runes[start:i]), col: start + 1, val: n})
		case isIdentStart(c):
			start := i
			for i < len(runes) && isIdentRune(runes[i]) {
				i++
			}
			toks = append(toks, token{kind: tIdent, text: string(runes[start:i]), col: start + 1})
		default:
			return nil, tokErr(line, i+1, fmt.Sprintf("unexpected character %q", c))
		}
	}
	return toks, nil
}

func tokErr(line string, col int, msg string) *Error {
	_ = line
	return &Error{Pos: Pos{Line: 0, Column: col}, Msg: msg}

}

func isDigit(c rune) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c rune) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentRune(c rune) bool  { return isIdentStart(c) || isDigit(c) }

func (p *lineParser) posOf(i int) Pos {
	col := 1
	if i < len(p.toks) {
		col = p.toks[i].col
	}
	return Pos{File: p.file, Line: p.line, Column: col}
}

func (p *lineParser) errf(col int, format string, args ...any) error {
	return &Error{Pos: Pos{File: p.file, Line: p.line, Column: col}, Msg: fmt.Sprintf(format, args...)}
}

func (p *lineParser) curTok() token {
	if p.pos >= len(p.toks) {
		return token{kind: tEOF, col: len(p.src) + 1}
	}
	return p.toks[p.pos]
}

func (p *lineParser) take(text string) bool {
	t := p.curTok()
	if t.kind == tPunct && t.text == text {
		p.pos++
		return true
	}
	return false
}

func (p *lineParser) expectPunct(text string) error {
	if !p.take(text) {
		t := p.curTok()
		return p.errf(t.col, "expected %q, got %q", text, t.text)
	}
	return nil
}

func (p *lineParser) expectIdent() (string, Pos, error) {
	t := p.curTok()
	if t.kind != tIdent {
		return "", Pos{}, p.errf(t.col, "expected identifier, got %q", t.text)
	}
	p.pos++
	return t.text, p.posOf(p.pos - 1), nil
}

func (p *lineParser) expectReg() (Reg, Pos, error) {
	t := p.curTok()
	if t.kind != tReg {
		return 0, Pos{}, p.errf(t.col, "expected virtual register vN, got %q", t.text)
	}
	p.pos++
	return Reg(t.val), p.posOf(p.pos - 1), nil
}

// parseInt consumes a signed integer literal ([+-]? digits).
func (p *lineParser) parseInt() (int64, error) {
	neg := false
	if t := p.curTok(); t.kind == tPunct && (t.text == "-" || t.text == "+") {
		neg = t.text == "-"
		p.pos++
	}
	t := p.curTok()
	if t.kind != tInt {
		return 0, p.errf(t.col, "expected integer literal, got %q", t.text)
	}
	p.pos++
	if neg {
		return -t.val, nil
	}
	return t.val, nil
}

func (p *lineParser) expectEnd() error {
	if p.pos != len(p.toks) {
		t := p.toks[p.pos]
		return p.errf(t.col, "unexpected trailing token %q", t.text)
	}
	return nil
}

func (p *lineParser) parseFuncHeader() (*Func, error) {
	if t := p.curTok(); !(t.kind == tIdent && t.text == "func") {
		return nil, p.errf(t.col, "expected function header")
	}
	p.pos++
	name, npos, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	f := &Func{Name: name, Pos: npos, blockNames: map[string]int{}}
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	if !p.take(")") {
		for {
			r, _, err := p.expectReg()
			if err != nil {
				return nil, err
			}
			f.Params = append(f.Params, r)
			if p.take(")") {
				break
			}
			if err := p.expectPunct(","); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expectPunct(":"); err != nil {
		return nil, err
	}
	if err := p.expectEnd(); err != nil {
		return nil, err
	}
	return f, nil
}

func (p *lineParser) parseLabel() (*Block, error) {
	name, npos, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	if err := p.expectPunct(":"); err != nil {
		return nil, err
	}
	if err := p.expectEnd(); err != nil {
		return nil, err
	}
	return &Block{Name: name, Pos: npos}, nil
}

func (p *lineParser) parseDef(ins *Inst) error {
	r, _, err := p.expectReg()
	if err != nil {
		return err
	}
	ins.Dst = r
	ins.HasDst = true
	return nil
}

func (p *lineParser) parseUse(ins *Inst, which int) error {
	r, _, err := p.expectReg()
	if err != nil {
		return err
	}
	switch which {
	case 1:
		ins.Src1 = r
		ins.HasSrc1 = true
	case 2:
		ins.Src2 = r
		ins.HasSrc2 = true
	}
	return nil
}

// parseMem parses [vN + imm] (imm defaults to 0). For stores the base is Dst.
func (p *lineParser) parseMem(ins *Inst, store bool) error {
	if err := p.expectPunct("["); err != nil {
		return err
	}
	base, _, err := p.expectReg()
	if err != nil {
		return err
	}
	if store {
		ins.Dst = base
		ins.HasDst = true
	} else {
		ins.Src1 = base
		ins.HasSrc1 = true
	}
	imm := int64(0)
	if t := p.curTok(); t.kind == tPunct && (t.text == "+" || t.text == "-") {
		v, err := p.parseInt()
		if err != nil {
			return err
		}
		imm = v
	}
	if err := p.expectPunct("]"); err != nil {
		return err
	}
	ins.Imm = imm
	return nil
}

// parseInst parses one instruction line. Grammar:
//
//	nop
//	vD = imm
//	vD = add|sub|... vA, vB
//	vD = vA
//	vD = [vBase + imm]
//	vD = fname(vA, vB, ...)
//	store vData, [vBase + imm]
//	ret [vA]
//	jmp label
//	br vCond, thenLabel, elseLabel
func (p *lineParser) parseInst() (*Inst, error) {
	t0 := p.curTok()
	switch {
	case t0.kind == tReg:
		return p.parseAssign()
	case t0.kind == tIdent && (t0.text == "store" || t0.text == "ret" ||
		t0.text == "jmp" || t0.text == "br" || t0.text == "nop"):
		return p.parseKeywordStmt()
	default:
		return nil, p.errf(t0.col, "expected instruction, got %q", t0.text)
	}
}

func (p *lineParser) parseAssign() (*Inst, error) {
	dst, dpos, err := p.expectReg()
	if err != nil {
		return nil, err
	}
	if err := p.expectPunct("="); err != nil {
		return nil, err
	}
	ins := &Inst{Pos: dpos, Dst: dst, HasDst: true}

	switch t := p.curTok(); {
	case t.kind == tPunct && t.text == "[":
		ins.Op = OpLoad
		if err := p.parseMem(ins, false); err != nil {
			return nil, err
		}
	case t.kind == tInt || (t.kind == tPunct && (t.text == "-" || t.text == "+")):
		ins.Op = OpConst
		imm, err := p.parseInt()
		if err != nil {
			return nil, err
		}
		ins.Imm = imm
	case t.kind == tIdent:
		name := t.text
		if op, ok := opByName[name]; ok && isBinaryOp(op) {
			ins.Op = op
			p.pos++
			if err := p.parseUse(ins, 1); err != nil {
				return nil, err
			}
			if err := p.expectPunct(","); err != nil {
				return nil, err
			}
			if err := p.parseUse(ins, 2); err != nil {
				return nil, err
			}
		} else if !ok {
			// Function call.
			ins.Op = OpCall
			ins.Func = name
			p.pos++
			if err := p.expectPunct("("); err != nil {
				return nil, err
			}
			if !p.take(")") {
				for {
					r, _, err := p.expectReg()
					if err != nil {
						return nil, err
					}
					ins.Args = append(ins.Args, r)
					if p.take(")") {
						break
					}
					if err := p.expectPunct(","); err != nil {
						return nil, err
					}
				}
			}
		} else {
			return nil, p.errf(t.col, "opcode %q cannot appear on the right of an assignment", name)
		}
	case t.kind == tReg:
		ins.Op = OpMove
		if err := p.parseUse(ins, 1); err != nil {
			return nil, err
		}
	default:
		return nil, p.errf(t.col, "expected literal, memory operand or expression, got %q", t.text)
	}

	if err := p.expectEnd(); err != nil {
		return nil, err
	}
	return ins, nil
}

func isBinaryOp(op Op) bool {
	switch op {
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpCmpLt, OpCmpLe, OpCmpEq, OpCmpNe:
		return true
	}
	return false
}

func (p *lineParser) parseKeywordStmt() (*Inst, error) {
	name, npos, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	op := opByName[name]
	ins := &Inst{Op: op, Pos: npos}

	switch op {
	case OpNop:
	case OpStore:
		if err := p.parseUse(ins, 1); err != nil {
			return nil, err
		}
		if err := p.expectPunct(","); err != nil {
			return nil, err
		}
		if err := p.parseMem(ins, true); err != nil {
			return nil, err
		}
	case OpRet:
		if p.pos < len(p.toks) {
			if err := p.parseUse(ins, 1); err != nil {
				return nil, err
			}
		}
	case OpJmp:
		label, _, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		ins.Label = label
	case OpBr:
		if err := p.parseUse(ins, 1); err != nil {
			return nil, err
		}
		if err := p.expectPunct(","); err != nil {
			return nil, err
		}
		t1, _, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(","); err != nil {
			return nil, err
		}
		t2, _, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		ins.Label = t1
		ins.Next = t2
	}
	if err := p.expectEnd(); err != nil {
		return nil, err
	}
	return ins, nil
}
