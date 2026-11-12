package content_forms

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Formulas of computed fields: numbers, field ids, + - * / and parentheses
// and the functions round(x[, digits]), min, max, abs, sum. Values of fields:
// numbers, true = 1, choices with numeric values, else 0. The browser shows
// the result while typing; the server computes it again and stores it.

type node interface {
	eval(vars map[string]float64) float64
}

type numNode float64
type varNode string
type negNode struct{ x node }
type binNode struct {
	op   rune
	a, b node
}
type callNode struct {
	fn   string
	args []node
}

func (n numNode) eval(map[string]float64) float64   { return float64(n) }
func (n varNode) eval(v map[string]float64) float64 { return v[string(n)] }
func (n negNode) eval(v map[string]float64) float64 { return -n.x.eval(v) }
func (n binNode) eval(v map[string]float64) float64 {
	a, b := n.a.eval(v), n.b.eval(v)
	switch n.op {
	case '+':
		return a + b
	case '-':
		return a - b
	case '*':
		return a * b
	default:
		if b == 0 {
			return 0 // no division by zero
		}
		return a / b
	}
}
func (n callNode) eval(v map[string]float64) float64 {
	xs := make([]float64, len(n.args))
	for i, a := range n.args {
		xs[i] = a.eval(v)
	}
	switch n.fn {
	case "round":
		d := 0.0
		if len(xs) > 1 {
			d = math.Max(0, math.Min(10, math.Round(xs[1])))
		}
		p := math.Pow(10, d)
		return math.Round(xs[0]*p) / p
	case "abs":
		return math.Abs(xs[0])
	case "min":
		m := xs[0]
		for _, x := range xs[1:] {
			m = math.Min(m, x)
		}
		return m
	case "max":
		m := xs[0]
		for _, x := range xs[1:] {
			m = math.Max(m, x)
		}
		return m
	default: // sum
		s := 0.0
		for _, x := range xs {
			s += x
		}
		return s
	}
}

type parser struct {
	src  []rune
	pos  int
	refs []string
	deep int
}

// parseFormula parses a formula and returns it with the field ids it uses.
func parseFormula(s string) (node, []string, error) {
	if len(s) > 500 {
		return nil, nil, errors.New("formula too long")
	}
	p := &parser{src: []rune(s)}
	n, err := p.expr()
	if err != nil {
		return nil, nil, err
	}
	p.space()
	if p.pos < len(p.src) {
		return nil, nil, fmt.Errorf("unexpected %q", string(p.src[p.pos]))
	}
	return n, p.refs, nil
}

func (p *parser) space() {
	for p.pos < len(p.src) && unicode.IsSpace(p.src[p.pos]) {
		p.pos++
	}
}

func (p *parser) peek() rune {
	p.space()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) expr() (node, error) {
	if p.deep++; p.deep > 50 {
		return nil, errors.New("formula nested too deeply")
	}
	defer func() { p.deep-- }()
	a, err := p.term()
	for err == nil {
		c := p.peek()
		if c != '+' && c != '-' {
			break
		}
		p.pos++
		var b node
		if b, err = p.term(); err == nil {
			a = binNode{c, a, b}
		}
	}
	return a, err
}

func (p *parser) term() (node, error) {
	a, err := p.factor()
	for err == nil {
		c := p.peek()
		if c != '*' && c != '/' {
			break
		}
		p.pos++
		var b node
		if b, err = p.factor(); err == nil {
			a = binNode{c, a, b}
		}
	}
	return a, err
}

func (p *parser) factor() (node, error) {
	c := p.peek()
	switch {
	case c == '-':
		p.pos++
		x, err := p.factor()
		return negNode{x}, err
	case c == '(':
		p.pos++
		x, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, errors.New("missing )")
		}
		p.pos++
		return x, nil
	case c >= '0' && c <= '9' || c == '.':
		st := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		f, err := strconv.ParseFloat(string(p.src[st:p.pos]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", string(p.src[st:p.pos]))
		}
		return numNode(f), nil
	case c >= 'a' && c <= 'z':
		st := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' || p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '_') {
			p.pos++
		}
		name := string(p.src[st:p.pos])
		if p.peek() == '(' {
			if !strings.Contains("|round|abs|min|max|sum|", "|"+name+"|") {
				return nil, fmt.Errorf("unknown function %q", name)
			}
			p.pos++
			var args []node
			for {
				a, err := p.expr()
				if err != nil {
					return nil, err
				}
				if args = append(args, a); len(args) > 50 {
					return nil, errors.New("too many arguments")
				}
				if p.peek() == ',' {
					p.pos++
					continue
				}
				break
			}
			if p.peek() != ')' {
				return nil, errors.New("missing )")
			}
			p.pos++
			if name == "abs" && len(args) != 1 || name == "round" && len(args) > 2 {
				return nil, fmt.Errorf("wrong number of arguments for %s", name)
			}
			return callNode{name, args}, nil
		}
		p.refs = append(p.refs, name)
		return varNode(name), nil
	}
	if c == 0 {
		return nil, errors.New("formula ends too early")
	}
	return nil, fmt.Errorf("unexpected %q", string(c))
}

// numeric returns the number a value stands for in formulas.
func numeric(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case bool:
		if t {
			return 1
		}
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(t, ",", "."), 64)
		if err == nil {
			return f
		}
	case []string:
		s := 0.0
		for _, x := range t {
			s += numeric(x)
		}
		return s
	}
	return 0
}
