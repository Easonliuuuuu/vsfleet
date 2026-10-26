// Package jsonpath implements the subset of kubectl's JSONPath templates that
// "-o jsonpath=", "-o custom-columns=" and "--sort-by" need, over values
// decoded from JSON (map[string]any, []any, string, float64, bool, nil).
//
// A template is text with {expressions} in it:
//
//	{.items[*].name}                 a path from the current object
//	{$.items[0]}                     a path from the root
//	{range .items[*]}{.name}{"\n"}{end}
//	{.items[?(@.cpu>=8)].name}       a filter
//
// Paths take .field, ['field'], [n] (negative counts from the end), [a:b],
// [*] and .*. Filters compare a path with a number, a quoted string, true,
// false or null, or test that a path exists. A missing field yields nothing
// rather than an error, as kubectl's default --allow-missing-template-keys
// does. Recursive descent (..) and unions ([a,b]) are not supported and say
// so.
package jsonpath

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Template is a parsed template.
type Template struct {
	nodes []node
}

type node interface{}

type textNode string

type exprNode struct{ path path }

type literalNode string

type rangeNode struct {
	path path
	body []node
}

// path is a sequence of steps from a starting point: the current object
// (".", "@") or the root ("$").
type path struct {
	fromRoot bool
	steps    []step
}

type stepKind int

const (
	stepField stepKind = iota
	stepIndex
	stepSlice
	stepWildcard
	stepFilter
)

type step struct {
	kind   stepKind
	field  string
	index  int
	lo, hi *int
	filter *filter
}

type filter struct {
	path  path
	op    string // "" tests existence
	value any
}

// Parse parses a template.
func Parse(text string) (*Template, error) {
	p := &parser{text: text}
	nodes, err := p.parseNodes(false)
	if err != nil {
		return nil, err
	}
	return &Template{nodes: nodes}, nil
}

// ParseExpr parses a single expression, with or without braces, such as
// ".name" or "{.name}", for custom columns and --sort-by.
func ParseExpr(expr string) (*Template, error) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "{") {
		expr = "{" + expr + "}"
	}
	t, err := Parse(expr)
	if err != nil {
		return nil, err
	}
	if len(t.nodes) != 1 {
		return nil, fmt.Errorf("%q is not a single expression", expr)
	}
	if _, ok := t.nodes[0].(exprNode); !ok {
		return nil, fmt.Errorf("%q is not a path expression", expr)
	}
	return t, nil
}

// Execute writes the template for data.
func (t *Template) Execute(w io.Writer, data any) error {
	var b strings.Builder
	if err := execute(&b, t.nodes, data, data); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Values returns what a single-expression template selects from data.
func (t *Template) Values(data any) []any {
	if len(t.nodes) != 1 {
		return nil
	}
	e, ok := t.nodes[0].(exprNode)
	if !ok {
		return nil
	}
	return e.path.eval(data, data)
}

func execute(b *strings.Builder, nodes []node, root, current any) error {
	for _, n := range nodes {
		switch n := n.(type) {
		case textNode:
			b.WriteString(string(n))
		case literalNode:
			b.WriteString(string(n))
		case exprNode:
			for i, v := range n.path.eval(root, current) {
				if i > 0 {
					b.WriteByte(' ')
				}
				s, err := Format(v)
				if err != nil {
					return err
				}
				b.WriteString(s)
			}
		case rangeNode:
			for _, item := range n.path.eval(root, current) {
				if err := execute(b, n.body, root, item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Format renders one selected value the way kubectl prints it: strings
// as-is, numbers without a trailing ".0", null as nothing, and objects and
// arrays as JSON.
func Format(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case json.Number:
		return v.String(), nil
	default:
		out, err := json.Marshal(v)
		return string(out), err
	}
}

func (p path) eval(root, current any) []any {
	values := []any{current}
	if p.fromRoot {
		values = []any{root}
	}
	for _, s := range p.steps {
		var next []any
		for _, v := range values {
			next = append(next, s.apply(root, v)...)
		}
		values = next
	}
	return values
}

func (s step) apply(root, v any) []any {
	switch s.kind {
	case stepField:
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		f, ok := m[s.field]
		if !ok {
			return nil
		}
		return []any{f}
	case stepIndex:
		a, ok := v.([]any)
		if !ok {
			return nil
		}
		i := s.index
		if i < 0 {
			i += len(a)
		}
		if i < 0 || i >= len(a) {
			return nil
		}
		return []any{a[i]}
	case stepSlice:
		a, ok := v.([]any)
		if !ok {
			return nil
		}
		lo, hi := 0, len(a)
		if s.lo != nil {
			lo = clampIndex(*s.lo, len(a))
		}
		if s.hi != nil {
			hi = clampIndex(*s.hi, len(a))
		}
		if lo >= hi {
			return nil
		}
		return append([]any(nil), a[lo:hi]...)
	case stepWildcard:
		switch v := v.(type) {
		case []any:
			return append([]any(nil), v...)
		case map[string]any:
			out := make([]any, 0, len(v))
			for _, key := range slices.Sorted(maps.Keys(v)) {
				out = append(out, v[key])
			}
			return out
		}
		return nil
	case stepFilter:
		a, ok := v.([]any)
		if !ok {
			return nil
		}
		var out []any
		for _, item := range a {
			if s.filter.match(root, item) {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

func clampIndex(i, n int) int {
	if i < 0 {
		i += n
	}
	return max(0, min(i, n))
}

func (f *filter) match(root, item any) bool {
	got := f.path.eval(root, item)
	if f.op == "" {
		return len(got) > 0 && got[0] != nil
	}
	if len(got) == 0 {
		return false
	}
	c, ok := Compare(got[0], f.value)
	if !ok {
		return f.op == "!="
	}
	switch f.op {
	case "==":
		return c == 0
	case "!=":
		return c != 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	}
	return false
}

// Compare orders two decoded JSON scalars: numbers numerically, strings
// lexically, false before true. ok is false when the two cannot be ordered,
// such as a number against a string.
func Compare(a, b any) (int, bool) {
	switch a := a.(type) {
	case float64:
		if b, ok := b.(float64); ok {
			switch {
			case a < b:
				return -1, true
			case a > b:
				return 1, true
			}
			return 0, true
		}
	case string:
		if b, ok := b.(string); ok {
			return strings.Compare(a, b), true
		}
	case bool:
		if b, ok := b.(bool); ok {
			switch {
			case a == b:
				return 0, true
			case !a:
				return -1, true
			}
			return 1, true
		}
	case nil:
		if b == nil {
			return 0, true
		}
	}
	return 0, false
}

// parser reads a template left to right.
type parser struct {
	text string
	pos  int
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("jsonpath %q at %d: %s", p.text, p.pos, fmt.Sprintf(format, args...))
}

// parseNodes reads until the end of the text, or until {end} when inRange.
func (p *parser) parseNodes(inRange bool) ([]node, error) {
	var nodes []node
	for p.pos < len(p.text) {
		open := strings.IndexByte(p.text[p.pos:], '{')
		if open < 0 {
			nodes = append(nodes, textNode(p.text[p.pos:]))
			p.pos = len(p.text)
			break
		}
		if open > 0 {
			nodes = append(nodes, textNode(p.text[p.pos:p.pos+open]))
		}
		p.pos += open + 1
		body, err := p.readAction()
		if err != nil {
			return nil, err
		}
		switch {
		case body == "end":
			if !inRange {
				return nil, p.errorf("{end} without {range}")
			}
			return nodes, nil
		case strings.HasPrefix(body, "range ") || body == "range":
			pa, err := parsePath(strings.TrimSpace(strings.TrimPrefix(body, "range")))
			if err != nil {
				return nil, p.errorf("%v", err)
			}
			inner, err := p.parseNodes(true)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, rangeNode{path: pa, body: inner})
		case strings.HasPrefix(body, `"`) || strings.HasPrefix(body, `'`):
			s, err := unquote(body)
			if err != nil {
				return nil, p.errorf("%v", err)
			}
			nodes = append(nodes, literalNode(s))
		default:
			pa, err := parsePath(body)
			if err != nil {
				return nil, p.errorf("%v", err)
			}
			nodes = append(nodes, exprNode{path: pa})
		}
	}
	if inRange {
		return nil, p.errorf("{range} without {end}")
	}
	return nodes, nil
}

// readAction reads up to the matching "}", skipping braces inside quotes.
func (p *parser) readAction() (string, error) {
	start := p.pos
	var quote byte
	for ; p.pos < len(p.text); p.pos++ {
		c := p.text[p.pos]
		switch {
		case quote != 0:
			if c == '\\' {
				p.pos++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '}':
			body := strings.TrimSpace(p.text[start:p.pos])
			p.pos++
			return body, nil
		}
	}
	return "", p.errorf("unclosed {")
}

func unquote(s string) (string, error) {
	if strings.HasPrefix(s, "'") {
		if len(s) < 2 || !strings.HasSuffix(s, "'") {
			return "", errors.New("unterminated string")
		}
		s = `"` + strings.ReplaceAll(strings.ReplaceAll(s[1:len(s)-1], `"`, `\"`), `\'`, `'`) + `"`
	}
	out, err := strconv.Unquote(s)
	if err != nil {
		return "", fmt.Errorf("invalid string %s", s)
	}
	return out, nil
}

// parsePath reads a path such as ".items[*].name", "$.a" or "@".
func parsePath(s string) (path, error) {
	var p path
	switch {
	case s == "":
		return p, errors.New("empty expression")
	case strings.HasPrefix(s, "$"):
		p.fromRoot = true
		s = s[1:]
	case strings.HasPrefix(s, "@"):
		s = s[1:]
	case !strings.HasPrefix(s, ".") && !strings.HasPrefix(s, "["):
		return p, fmt.Errorf("expression %q must start with ., [, @ or $", s)
	}
	for len(s) > 0 {
		switch {
		case strings.HasPrefix(s, ".."):
			return p, errors.New("recursive descent (..) is not supported")
		case strings.HasPrefix(s, ".*"):
			p.steps = append(p.steps, step{kind: stepWildcard})
			s = s[2:]
		case strings.HasPrefix(s, "."):
			s = s[1:]
			n := strings.IndexAny(s, ".[")
			if n < 0 {
				n = len(s)
			}
			if n == 0 {
				// A bare "." selects the current object.
				if len(s) == 0 {
					return p, nil
				}
				return p, errors.New("empty field name")
			}
			p.steps = append(p.steps, step{kind: stepField, field: s[:n]})
			s = s[n:]
		case strings.HasPrefix(s, "["):
			end := closingBracket(s)
			if end < 0 {
				return p, errors.New("unclosed [")
			}
			st, err := parseBracket(strings.TrimSpace(s[1:end]))
			if err != nil {
				return p, err
			}
			p.steps = append(p.steps, st)
			s = s[end+1:]
		default:
			return p, fmt.Errorf("unexpected %q", s)
		}
	}
	return p, nil
}

// closingBracket finds the "]" that closes s[0], skipping quoted text and
// nested brackets inside a filter.
func closingBracket(s string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseBracket(s string) (step, error) {
	switch {
	case s == "*":
		return step{kind: stepWildcard}, nil
	case strings.HasPrefix(s, "?("):
		if !strings.HasSuffix(s, ")") {
			return step{}, errors.New("unclosed filter")
		}
		f, err := parseFilter(strings.TrimSpace(s[2 : len(s)-1]))
		if err != nil {
			return step{}, err
		}
		return step{kind: stepFilter, filter: f}, nil
	case strings.HasPrefix(s, "'") || strings.HasPrefix(s, `"`):
		name, err := unquote(s)
		if err != nil {
			return step{}, err
		}
		return step{kind: stepField, field: name}, nil
	case strings.Contains(s, ","):
		return step{}, errors.New("unions ([a,b]) are not supported")
	case strings.Contains(s, ":"):
		lo, hi, _ := strings.Cut(s, ":")
		if strings.Contains(hi, ":") {
			return step{}, errors.New("slice steps are not supported")
		}
		st := step{kind: stepSlice}
		for _, bound := range []struct {
			text string
			dst  **int
		}{{lo, &st.lo}, {hi, &st.hi}} {
			if t := strings.TrimSpace(bound.text); t != "" {
				n, err := strconv.Atoi(t)
				if err != nil {
					return step{}, fmt.Errorf("invalid slice bound %q", t)
				}
				*bound.dst = &n
			}
		}
		return st, nil
	default:
		n, err := strconv.Atoi(s)
		if err != nil {
			return step{}, fmt.Errorf("invalid index %q", s)
		}
		return step{kind: stepIndex, index: n}, nil
	}
}

func parseFilter(s string) (*filter, error) {
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		left, right, ok := cutOutsideQuotes(s, op)
		if !ok {
			continue
		}
		pa, err := parsePath(strings.TrimSpace(left))
		if err != nil {
			return nil, err
		}
		v, err := parseLiteral(strings.TrimSpace(right))
		if err != nil {
			return nil, err
		}
		return &filter{path: pa, op: op, value: v}, nil
	}
	pa, err := parsePath(s)
	if err != nil {
		return nil, err
	}
	return &filter{path: pa}, nil
}

// cutOutsideQuotes splits s at the first op that is not inside quotes.
func cutOutsideQuotes(s, op string) (string, string, bool) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case strings.HasPrefix(s[i:], op):
			return s[:i], s[i+len(op):], true
		}
	}
	return "", "", false
}

func parseLiteral(s string) (any, error) {
	switch {
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	case s == "null":
		return nil, nil
	case strings.HasPrefix(s, "'") || strings.HasPrefix(s, `"`):
		return unquote(s)
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("filter value %q is not a number, a quoted string, true, false or null", s)
	}
	return n, nil
}
