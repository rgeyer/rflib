package query

import "strings"

type tokKind int

const (
	tEOF tokKind = iota
	tLParen
	tRParen
	tOr
	tAnd
	tNot
	tWord
)

type token struct {
	kind tokKind
	pos  int    // 1-based
	raw  string // as written
	// Words: field and value, unquoted. hasColon separates field:value
	// from bare text; quoted and quotedValue record quoting, which turns
	// off keywords, @references, and comparisons.
	field, value        string
	hasColon            bool
	quoted, quotedValue bool
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func lex(s string) ([]token, error) {
	var toks []token
	for i := 0; i < len(s); {
		switch b := s[i]; {
		case isSpace(b):
			i++
		case b == '(':
			toks = append(toks, token{kind: tLParen, pos: i + 1, raw: "("})
			i++
		case b == ')':
			toks = append(toks, token{kind: tRParen, pos: i + 1, raw: ")"})
			i++
		case b == '-' && i+1 < len(s) && !isSpace(s[i+1]) && s[i+1] != ')':
			toks = append(toks, token{kind: tNot, pos: i + 1, raw: "-"})
			i++
		default:
			t, end, err := lexWord(s, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, t)
			i = end
		}
	}
	return append(toks, token{kind: tEOF, pos: len(s) + 1}), nil
}

// lexWord reads a word: everything up to a space or parenthesis outside
// quotes. The first unquoted colon splits field from value.
func lexWord(s string, start int) (token, int, error) {
	t := token{kind: tWord, pos: start + 1}
	var buf strings.Builder
	i := start
	for i < len(s) {
		b := s[i]
		if isSpace(b) || b == '(' || b == ')' {
			break
		}
		switch {
		case b == '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				buf.WriteByte(s[j])
			}
			if j >= len(s) {
				return t, 0, errorAt(i+1, "missing closing quote")
			}
			if t.hasColon {
				t.quotedValue = true
			} else {
				t.quoted = true
			}
			i = j + 1
		case b == ':' && !t.hasColon && !t.quoted:
			t.field, t.hasColon = buf.String(), true
			buf.Reset()
			i++
		default:
			buf.WriteByte(b)
			i++
		}
	}
	t.value, t.raw = buf.String(), s[start:i]
	if !t.hasColon && !t.quoted {
		switch strings.ToUpper(t.value) {
		case "OR":
			t.kind = tOr
		case "AND":
			t.kind = tAnd
		case "NOT":
			t.kind = tNot
		}
	}
	return t, i, nil
}
