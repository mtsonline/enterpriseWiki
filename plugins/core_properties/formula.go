package core_properties

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Formulas compute a number from other properties of the same element:
//
//	price * quantity
//	round(done / total * 100)
//	if(end - start > 30, 1, 0)
//
// Numbers, + - * / %, comparisons (< <= > >= == !=, giving 1 or 0), and
// && || !, parentheses, property keys (numbers; dates as days since
// 1970-01-01; yes/no as 1/0; empty as 0) and the functions min, max, abs,
// round(x[, decimals]), floor, ceil, sqrt, if(condition, then, else).
// The parser builds a tree once; evaluation has no loops, no access to
// anything but the given values and a fixed size, so it is safe and fast.

// Formula is a parsed formula.
type Formula struct {
	root node
	keys []string
}

// Keys are the properties a formula reads.
func (f *Formula) Keys() []string { return f.keys }

type node interface {
	eval(vals map[string]float64) (float64, error)
}

type numNode float64
type keyNode string
type unNode struct {
	op string
	x  node
}
type binNode struct {
	op   string
	l, r node
}
type callNode struct {
	fn   string
	args []node
}

var errDivZero = errors.New("division by zero")

func (n numNode) eval(map[string]float64) (float64, error) { return float64(n), nil }
func (n keyNode) eval(v map[string]float64) (float64, error) {
	return v[string(n)], nil
}
func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func (n unNode) eval(v map[string]float64) (float64, error) {
	x, err := n.x.eval(v)
	if err != nil {
		return 0, err
	}
	if n.op == "!" {
		return b2f(x == 0), nil
	}
	return -x, nil
}
func (n binNode) eval(v map[string]float64) (float64, error) {
	l, err := n.l.eval(v)
	if err != nil {
		return 0, err
	}
	// short-circuit
	switch n.op {
	case "&&":
		if l == 0 {
			return 0, nil
		}
	case "||":
		if l != 0 {
			return 1, nil
		}
	}
	r, err := n.r.eval(v)
	if err != nil {
		return 0, err
	}
	switch n.op {
	case "+":
		return l + r, nil
	case "-":
		return l - r, nil
	case "*":
		return l * r, nil
	case "/":
		if r == 0 {
			return 0, errDivZero
		}
		return l / r, nil
	case "%":
		if r == 0 {
			return 0, errDivZero
		}
		return math.Mod(l, r), nil
	case "<":
		return b2f(l < r), nil
	case "<=":
		return b2f(l <= r), nil
	case ">":
		return b2f(l > r), nil
	case ">=":
		return b2f(l >= r), nil
	case "==":
		return b2f(l == r), nil
	case "!=":
		return b2f(l != r), nil
	case "&&", "||":
		return b2f(r != 0), nil
	}
	return 0, fmt.Errorf("operator %s", n.op)
}

var funcs = map[string][2]int{ // min and max number of arguments
	"min": {1, 20}, "max": {1, 20}, "abs": {1, 1}, "round": {1, 2}, "floor": {1, 1}, "ceil": {1, 1}, "sqrt": {1, 1}, "if": {3, 3},
}

func (n callNode) eval(v map[string]float64) (float64, error) {
	if n.fn == "if" {
		c, err := n.args[0].eval(v)
		if err != nil {
			return 0, err
		}
		if c != 0 {
			return n.args[1].eval(v)
		}
		return n.args[2].eval(v)
	}
	xs := make([]float64, len(n.args))
	for i, a := range n.args {
		x, err := a.eval(v)
		if err != nil {
			return 0, err
		}
		xs[i] = x
	}
	switch n.fn {
	case "min":
		m := xs[0]
		for _, x := range xs[1:] {
			m = math.Min(m, x)
		}
		return m, nil
	case "max":
		m := xs[0]
		for _, x := range xs[1:] {
			m = math.Max(m, x)
		}
		return m, nil
	case "abs":
		return math.Abs(xs[0]), nil
	case "floor":
		return math.Floor(xs[0]), nil
	case "ceil":
		return math.Ceil(xs[0]), nil
	case "sqrt":
		if xs[0] < 0 {
			return 0, errors.New("square root of a negative number")
		}
		return math.Sqrt(xs[0]), nil
	case "round":
		d := 0.0
		if len(xs) == 2 {
			d = math.Max(0, math.Min(6, math.Round(xs[1])))
		}
		p := math.Pow(10, d)
		return math.Round(xs[0]*p) / p, nil
	}
	return 0, fmt.Errorf("function %s", n.fn)
}

// Eval computes the formula; a result that is not a finite number is an error.
func (f *Formula) Eval(vals map[string]float64) (float64, error) {
	x, err := f.root.eval(vals)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0, errors.New("result is not a number")
	}
	return x, nil
}

// ---------------------------------------------------------------- parser

type parser struct {
	toks  []string
	pos   int
	depth int
	keys  map[string]bool
}

const (
	maxFormulaLen   = 500
	maxFormulaDepth = 40
)

func tokenize(s string) ([]string, error) {
	var out []string
	r := []rune(s)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case unicode.IsDigit(c) || c == '.':
			j := i
			for j < len(r) && (unicode.IsDigit(r[j]) || r[j] == '.') {
				j++
			}
			out = append(out, string(r[i:j]))
			i = j
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(r) && (r[j] == '_' || r[j] >= 'a' && r[j] <= 'z' || r[j] >= 'A' && r[j] <= 'Z' || unicode.IsDigit(r[j])) {
				j++
			}
			out = append(out, strings.ToLower(string(r[i:j])))
			i = j
		default:
			two := ""
			if i+1 < len(r) {
				two = string(r[i : i+2])
			}
			switch two {
			case "<=", ">=", "==", "!=", "&&", "||":
				out = append(out, two)
				i += 2
				continue
			}
			if strings.ContainsRune("+-*/%()<>!,", c) {
				out = append(out, string(c))
				i++
				continue
			}
			return nil, fmt.Errorf("unexpected %q", string(c))
		}
	}
	return out, nil
}

// ParseFormula parses a formula.
func ParseFormula(s string) (*Formula, error) {
	if len(s) > maxFormulaLen {
		return nil, errors.New("too long")
	}
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("empty")
	}
	p := &parser{toks: toks, keys: map[string]bool{}}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unexpected %q", p.toks[p.pos])
	}
	f := &Formula{root: n}
	for k := range p.keys {
		f.keys = append(f.keys, k)
	}
	return f, nil
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxFormulaDepth {
		return errors.New("too deeply nested")
	}
	return nil
}

func (p *parser) binary(next func() (node, error), ops ...string) (node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	l, err := next()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		found := false
		for _, o := range ops {
			found = found || o == op
		}
		if !found {
			return l, nil
		}
		p.pos++
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = binNode{op, l, r}
	}
}

func (p *parser) or() (node, error)  { return p.binary(p.and, "||") }
func (p *parser) and() (node, error) { return p.binary(p.cmp, "&&") }
func (p *parser) cmp() (node, error) { return p.binary(p.add, "<", "<=", ">", ">=", "==", "!=") }
func (p *parser) add() (node, error) { return p.binary(p.mul, "+", "-") }
func (p *parser) mul() (node, error) { return p.binary(p.unary, "*", "/", "%") }

func (p *parser) unary() (node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	switch p.peek() {
	case "-", "!":
		op := p.peek()
		p.pos++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return unNode{op, x}, nil
	case "+":
		p.pos++
		return p.unary()
	}
	return p.primary()
}

func (p *parser) primary() (node, error) {
	t := p.peek()
	switch {
	case t == "":
		return nil, errors.New("incomplete")
	case t == "(":
		p.pos++
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, errors.New("missing )")
		}
		p.pos++
		return n, nil
	case t[0] >= '0' && t[0] <= '9' || t[0] == '.':
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", t)
		}
		p.pos++
		return numNode(f), nil
	case t[0] == '_' || t[0] >= 'a' && t[0] <= 'z':
		p.pos++
		if p.peek() != "(" {
			if len(t) > 63 {
				return nil, errors.New("name too long")
			}
			p.keys[t] = true
			return keyNode(t), nil
		}
		lim, ok := funcs[t]
		if !ok {
			return nil, fmt.Errorf("unknown function %q", t)
		}
		p.pos++
		var args []node
		for p.peek() != ")" {
			a, err := p.or()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
			if p.peek() == "," {
				p.pos++
				continue
			}
			if p.peek() != ")" {
				return nil, errors.New("missing )")
			}
		}
		p.pos++
		if len(args) < lim[0] || len(args) > lim[1] {
			return nil, fmt.Errorf("%s: wrong number of arguments", t)
		}
		return callNode{t, args}, nil
	}
	return nil, fmt.Errorf("unexpected %q", t)
}
