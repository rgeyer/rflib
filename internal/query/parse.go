// Package query is rflib's channel search language, in the style of Gmail
// search:
//
//	service:none freq:144..148 -tag:hf (is:repeater OR band:70cm) @local
//
// Terms are field:value. A space means AND; OR binds tighter than AND, so
// "a b OR c" is a AND (b OR c). A leading - (or NOT) negates a term or a
// parenthesized group. Bare words match names, short names, and notes.
// @name uses a saved search.
//
// Parse turns text into a syntax tree; Compile turns a tree into a
// Matcher. The tree is the contract shared by every front end, and
// String prints it back in canonical form.
package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// Node is a parsed query.
type Node interface {
	String() string
}

// And matches when every node matches; an empty And matches everything.
type And struct{ Nodes []Node }

// Or matches when any node matches.
type Or struct{ Nodes []Node }

type Not struct{ Node Node }

// Term is field:value, with an optional comparison (>, >=, <, <=) before
// the value.
type Term struct {
	Field, Op, Value string
	// Pos is the term's 1-based character offset in the query.
	Pos int
}

// Text is a bare word or quoted phrase.
type Text struct {
	Value string
	Pos   int
}

// Ref uses a saved search by name.
type Ref struct {
	Name string
	Pos  int
}

// Error is a problem at a position in the query text.
type Error struct {
	Pos int
	Msg string
}

// Is makes a query error domain.ErrInvalid: the query is the caller's to fix.
func (e *Error) Is(target error) bool { return target == domain.ErrInvalid }

func (e *Error) Error() string {
	if e.Pos > 0 {
		return fmt.Sprintf("query: %s (at character %d)", e.Msg, e.Pos)
	}
	return "query: " + e.Msg
}

func errorAt(pos int, format string, args ...any) error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// Parse parses query text. Empty text is an empty And, which matches every
// channel.
func Parse(text string) (Node, error) {
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := parser{toks: toks}
	n, err := p.and()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, errorAt(t.pos, "unexpected %q", t.raw)
	}
	return n, nil
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

// and := or { [AND] or }
func (p *parser) and() (Node, error) {
	var nodes []Node
	for {
		t := p.peek()
		if t.kind == tEOF || t.kind == tRParen {
			break
		}
		if t.kind == tAnd {
			if len(nodes) == 0 {
				return nil, errorAt(t.pos, "AND needs a term before it")
			}
			p.next()
			if k := p.peek().kind; k == tEOF || k == tRParen || k == tOr || k == tAnd {
				return nil, errorAt(t.pos, "AND needs a term after it")
			}
			continue
		}
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 1 {
		return nodes[0], nil
	}
	return And{Nodes: nodes}, nil
}

// or := unary { OR unary }
func (p *parser) or() (Node, error) {
	if t := p.peek(); t.kind == tOr {
		return nil, errorAt(t.pos, "OR needs a term before it")
	}
	first, err := p.unary()
	if err != nil {
		return nil, err
	}
	nodes := []Node{first}
	for p.peek().kind == tOr {
		op := p.next()
		if k := p.peek().kind; k == tEOF || k == tRParen || k == tOr || k == tAnd {
			return nil, errorAt(op.pos, "OR needs a term after it")
		}
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 1 {
		return first, nil
	}
	return Or{Nodes: nodes}, nil
}

// unary := ('-' | NOT) unary | primary
func (p *parser) unary() (Node, error) {
	if t := p.peek(); t.kind == tNot {
		p.next()
		if k := p.peek().kind; k == tEOF || k == tRParen || k == tOr || k == tAnd {
			return nil, errorAt(t.pos, "%s needs a term after it", t.raw)
		}
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Not{Node: n}, nil
	}
	return p.primary()
}

// primary := '(' and ')' | word
func (p *parser) primary() (Node, error) {
	t := p.next()
	switch t.kind {
	case tLParen:
		n, err := p.and()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tRParen {
			return nil, errorAt(t.pos, "missing ) for this (")
		}
		if a, ok := n.(And); ok && len(a.Nodes) == 0 {
			return nil, errorAt(t.pos, "empty ( )")
		}
		return n, nil
	case tWord:
		return wordNode(t)
	case tRParen:
		return nil, errorAt(t.pos, "unexpected )")
	}
	return nil, errorAt(t.pos, "unexpected %q", t.raw)
}

func wordNode(t token) (Node, error) {
	if !t.hasColon {
		if !t.quoted && strings.HasPrefix(t.value, "@") {
			name := strings.ToLower(t.value[1:])
			if !domain.ValidSearchName(name) {
				return nil, errorAt(t.pos, "%q is not a saved search name; names use letters, digits, -, _, and .", t.value)
			}
			return Ref{Name: name, Pos: t.pos}, nil
		}
		return Text{Value: t.value, Pos: t.pos}, nil
	}
	field := strings.ToLower(t.field)
	if alias, ok := fieldAliases[field]; ok {
		field = alias
	}
	if !slices.Contains(Fields, field) {
		return nil, errorAt(t.pos, "unknown field %q; fields are %s", t.field, strings.Join(Fields, ", "))
	}
	term := Term{Field: field, Value: t.value, Pos: t.pos}
	if !t.quotedValue {
		for _, op := range []string{">=", "<=", ">", "<"} {
			if v, ok := strings.CutPrefix(t.value, op); ok {
				term.Op, term.Value = op, v
				break
			}
		}
	}
	if term.Value == "" && !t.quotedValue {
		return nil, errorAt(t.pos, "%s: needs a value", field)
	}
	return term, nil
}

// Scopes are the values of in:, which chooses the channels a query
// searches: those in use (use, the default: the library's own channels and
// the shared channels it annotates or has on a radio), only the library's
// own (mine), the whole shared list (shared), or both (all).
var Scopes = []string{"use", "mine", "shared", "all"}

// SplitScope takes in: terms off the top level of n and returns the scope
// they choose (empty when there are none) and the rest of the query. An in:
// anywhere else is left for Compile to reject.
func SplitScope(n Node) (string, Node, error) {
	scope := ""
	take := func(t Term) (bool, error) {
		if t.Field != "in" {
			return false, nil
		}
		v := strings.ToLower(strings.TrimSpace(t.Value))
		if !slices.Contains(Scopes, v) {
			return false, &Error{Pos: t.Pos, Msg: fmt.Sprintf("in:%s: use in:use, in:mine, in:shared, or in:all", t.Value)}
		}
		if scope != "" && scope != v {
			return false, &Error{Pos: t.Pos, Msg: "a query can search only one in: scope"}
		}
		scope = v
		return true, nil
	}
	switch x := n.(type) {
	case Term:
		if ok, err := take(x); ok || err != nil {
			// The scope alone matches everything in it.
			return scope, And{}, err
		}
	case And:
		var rest []Node
		for _, c := range x.Nodes {
			if t, isTerm := c.(Term); isTerm {
				ok, err := take(t)
				if err != nil {
					return "", nil, err
				}
				if ok {
					continue
				}
			}
			rest = append(rest, c)
		}
		return scope, Join(rest...), nil
	}
	return scope, n, nil
}

// MapTerms returns n with every Term replaced by fn's result.
func MapTerms(n Node, fn func(Term) Term) Node {
	each := func(ns []Node) []Node {
		out := make([]Node, len(ns))
		for i, c := range ns {
			out[i] = MapTerms(c, fn)
		}
		return out
	}
	switch x := n.(type) {
	case And:
		return And{Nodes: each(x.Nodes)}
	case Or:
		return Or{Nodes: each(x.Nodes)}
	case Not:
		return Not{Node: MapTerms(x.Node, fn)}
	case Term:
		return fn(x)
	}
	return n
}

// Refs returns the saved searches a query uses directly, in order, without
// repeats.
func Refs(n Node) []string {
	var out []string
	var walk func(Node)
	walk = func(n Node) {
		switch x := n.(type) {
		case And:
			for _, c := range x.Nodes {
				walk(c)
			}
		case Or:
			for _, c := range x.Nodes {
				walk(c)
			}
		case Not:
			walk(x.Node)
		case Ref:
			if !slices.Contains(out, x.Name) {
				out = append(out, x.Name)
			}
		}
	}
	walk(n)
	return out
}

// Join ANDs nodes, dropping empty Ands, for combining a query with
// shorthand flags.
func Join(nodes ...Node) Node {
	var out []Node
	for _, n := range nodes {
		switch x := n.(type) {
		case nil:
		case And:
			out = append(out, x.Nodes...)
		default:
			out = append(out, n)
		}
	}
	if len(out) == 1 {
		return out[0]
	}
	return And{Nodes: out}
}

// String parenthesizes OR inside AND. It isn't needed, since OR binds
// tighter, but it reads the same to people who expect the opposite.
func (a And) String() string {
	parts := make([]string, len(a.Nodes))
	for i, n := range a.Nodes {
		if _, ok := n.(Or); ok {
			parts[i] = "(" + n.String() + ")"
		} else {
			parts[i] = n.String()
		}
	}
	return strings.Join(parts, " ")
}

func (o Or) String() string {
	parts := make([]string, len(o.Nodes))
	for i, n := range o.Nodes {
		// AND inside OR needs parentheses; OR inside OR doesn't.
		if a, ok := n.(And); ok && len(a.Nodes) > 1 {
			parts[i] = "(" + n.String() + ")"
		} else {
			parts[i] = n.String()
		}
	}
	return strings.Join(parts, " OR ")
}

func (n Not) String() string {
	switch x := n.Node.(type) {
	case Or:
		return "-(" + x.String() + ")"
	case And:
		if len(x.Nodes) > 1 {
			return "-(" + x.String() + ")"
		}
	}
	return "-" + n.Node.String()
}

func (t Term) String() string { return t.Field + ":" + t.Op + quoteValue(t.Value, t.Op == "") }

func (t Text) String() string {
	if t.Value == "" || needsQuotes(t.Value) || strings.Contains(t.Value, ":") || strings.ContainsAny(t.Value[:1], "@-") ||
		t.Value == "OR" || t.Value == "AND" || t.Value == "NOT" {
		return quote(t.Value)
	}
	return t.Value
}

func (r Ref) String() string { return "@" + r.Name }

// quoteValue quotes a term value when it would otherwise lex differently.
// A value that starts like a comparison is quoted unless it carries one.
func quoteValue(v string, noOp bool) string {
	if v == "" || needsQuotes(v) || noOp && strings.ContainsAny(v[:1], "<>") {
		return quote(v)
	}
	return v
}

func needsQuotes(v string) bool {
	return strings.ContainsAny(v, " \t\n\"()")
}

func quote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}
