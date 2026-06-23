package mdparser

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	gmast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	// shortcodeInlineLimit is the display-width threshold below which a
	// block-level shortcode is kept on a single line.  At or above it, the
	// shortcode is expanded with one argument per line.
	shortcodeInlineLimit = 40
	// shortcodeIndent is the indentation applied to each argument line of an
	// expanded shortcode.
	shortcodeIndent = "    "
)

// KindShortcode is the AST node kind for a Hugo shortcode span.
var KindShortcode = gmast.NewNodeKind("HugoShortcode")

// shortcodeNode is an inline node holding a Hugo shortcode verbatim
// (e.g. {{< ref "x" >}} or {{% note %}}).  It is emitted as a single
// unbreakable unit so word wrapping never splits or reflows it.
type shortcodeNode struct {
	gmast.BaseInline
	Value []byte
}

func (n *shortcodeNode) Kind() gmast.NodeKind { return KindShortcode }

func (n *shortcodeNode) Dump(source []byte, level int) {
	gmast.DumpHelper(n, source, level, map[string]string{"Value": string(n.Value)}, nil)
}

// shortcodeWS matches a source line break together with any horizontal
// whitespace around it.  Line breaks inside a shortcode are collapsed to a
// single space so the shortcode renders on one line; spaces that appear
// within a single source line are left untouched.
var shortcodeWS = regexp.MustCompile(`[ \t]*\r?\n[ \t]*`)

// shortcodeParser recognizes Hugo shortcodes ({{< … >}} and {{% … %}})
// as atomic inline nodes.  It is registered as an inline parser triggered
// by '{'.
type shortcodeParser struct{}

func (p *shortcodeParser) Trigger() []byte { return []byte{'{'} }

func (p *shortcodeParser) Parse(parent gmast.Node, block text.Reader, pc parser.Context) gmast.Node {
	line, startSeg := block.PeekLine()
	var closer []byte
	switch {
	case bytes.HasPrefix(line, []byte("{{<")):
		closer = []byte(">}}")
	case bytes.HasPrefix(line, []byte("{{%")):
		closer = []byte("%}}")
	default:
		return nil
	}

	source := block.Source()
	start := startSeg.Start
	startLine, startPos := block.Position()

	// Consume the opener so the closer scan begins past it.
	block.Advance(3)

	// Scan forward (possibly across lines, but never past the end of the
	// current block) for the matching closer.
	for {
		line, seg := block.PeekLine()
		if line == nil {
			// Unterminated: leave the opener to be handled as text.
			block.SetPosition(startLine, startPos)
			return nil
		}
		if idx := bytes.Index(line, closer); idx >= 0 {
			end := seg.Start + idx + len(closer)
			block.Advance(idx + len(closer))
			value := shortcodeWS.ReplaceAll(source[start:end], []byte(" "))
			return &shortcodeNode{Value: value}
		}
		block.AdvanceLine()
	}
}

// shortcodeExtension registers the shortcode inline parser with Goldmark.
type shortcodeExtension struct{}

func (shortcodeExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&shortcodeParser{}, 100),
	))
}

// emitShortcode writes a block-level shortcode.  Short shortcodes are emitted
// on a single line; longer ones are expanded with the opening delimiter and
// name on the first line, each argument indented on its own line, and the
// closing delimiter on the last line.
func (mr *mdNodeRenderer) emitShortcode(value []byte) {
	p := mr.prefix()
	s := string(value)

	open, name, args, closer := splitShortcode(s)
	// Keep it on one line if it is short, or if there is nothing to expand
	// onto separate lines (no name, or no arguments).
	if displayWidth(s) < shortcodeInlineLimit || name == "" || len(args) == 0 {
		mr.emit(p + s + "\n")
		return
	}

	mr.emit(p + open + " " + name + "\n")
	for i, arg := range args {
		if i == len(args)-1 {
			// Closing delimiter rides on the last argument's line.
			mr.emit(p + shortcodeIndent + arg + " " + closer + "\n")
		} else {
			mr.emit(p + shortcodeIndent + arg + "\n")
		}
	}
}

// splitShortcode breaks a shortcode into its opening delimiter, name, argument
// tokens, and closing delimiter.  value must include the delimiters.
func splitShortcode(value string) (open, name string, args []string, closer string) {
	open = value[:3]
	closer = value[len(value)-3:]
	inner := strings.TrimSpace(value[3 : len(value)-3])
	tokens := tokenizeShortcode(inner)
	if len(tokens) == 0 {
		return open, "", nil, closer
	}
	return open, tokens[0], tokens[1:], closer
}

// tokenizeShortcode splits a shortcode's inner content into whitespace-
// separated tokens, keeping double-quoted ("…", with \" escapes) and
// backtick-quoted (`…`) values — which may contain spaces — intact.
func tokenizeShortcode(s string) []string {
	var tokens []string
	i, n := 0, len(s)
	for i < n {
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && s[i] != ' ' && s[i] != '\t' {
			switch s[i] {
			case '"':
				i++
				for i < n && s[i] != '"' {
					if s[i] == '\\' && i+1 < n {
						i += 2
					} else {
						i++
					}
				}
				if i < n {
					i++ // closing quote
				}
			case '`':
				i++
				for i < n && s[i] != '`' {
					i++
				}
				if i < n {
					i++ // closing backtick
				}
			default:
				i++
			}
		}
		tokens = append(tokens, s[start:i])
	}
	return tokens
}
