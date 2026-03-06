// Package mdparser provides a Markdown parser backed by Goldmark that
// returns an mdio.Renderable which can re-emit reformatted Markdown.
//
// Rendering uses Goldmark's renderer.NodeRenderer framework for dispatch,
// with a prefix stack to handle nesting (blockquotes, list indentation).
package mdparser

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	gmast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gmext "github.com/yuin/goldmark/extension/ast"
	gmrenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Option is a functional option for configuring a Parser.
type Option func(*Parser)

// WithWidth sets the line width for wrapping.
func WithWidth(width int) Option {
	return func(p *Parser) {
		p.width = width
	}
}

// WithOneSpaceAfterSentence disables two spaces after sentence-ending punctuation.
func WithOneSpaceAfterSentence(v bool) Option {
	return func(p *Parser) {
		p.oneSpaceAfterSentence = v
	}
}

// WithFrontmatter controls whether Hugo-style frontmatter is detected and
// preserved.  The default is true.
func WithFrontmatter(v bool) Option {
	return func(p *Parser) {
		p.frontmatter = v
	}
}

// WarnFunc is a callback used to emit warnings.
type WarnFunc func(format string, args ...interface{})

// WithWarnFunc sets a warning callback.  If nil, warnings are suppressed.
func WithWarnFunc(f WarnFunc) Option {
	return func(p *Parser) {
		p.warn = f
	}
}

// Parser parses Markdown source into a Renderable using Goldmark.
type Parser struct {
	width                 int
	oneSpaceAfterSentence bool
	frontmatter           bool
	warn                  WarnFunc
}

// NewParser creates a Parser with the given options.
// Default width is 79.  Two spaces after sentences is the default.
// Frontmatter detection is enabled by default.
func NewParser(opts ...Option) *Parser {
	p := &Parser{width: 79, frontmatter: true}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Parse parses source into a renderable Markdown document.
func (p *Parser) Parse(source []byte) (*Renderable, error) {
	var fmBytes []byte
	body := source

	detected := detectFrontmatterFormat(source)
	if p.frontmatter && detected != fmNone {
		var err error
		fmBytes, body, err = extractFrontmatter(source)
		if err != nil {
			return nil, err
		}
	} else if !p.frontmatter && detected != fmNone {
		if p.warn != nil {
			p.warn("frontmatter detected but --no-frontmatter is set; treating as plain markdown")
		}
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
		),
	)
	reader := text.NewReader(body)
	doc := md.Parser().Parse(reader)
	return &Renderable{
		doc:                   doc,
		source:                body,
		width:                 p.width,
		oneSpaceAfterSentence: p.oneSpaceAfterSentence,
		frontmatter:           fmBytes,
	}, nil
}

// Renderable holds a parsed Goldmark AST, the original source, and
// cached rendering options.
type Renderable struct {
	doc                   gmast.Node
	source                []byte
	width                 int
	oneSpaceAfterSentence bool
	frontmatter           []byte // formatted frontmatter to prepend
}

// Render writes reformatted Markdown to w.
// It creates a fresh Goldmark renderer with our NodeRenderer for each call.
func (r *Renderable) Render(w io.Writer) error {
	hasFM := len(r.frontmatter) > 0
	if hasFM {
		if _, err := w.Write(r.frontmatter); err != nil {
			return err
		}
	}
	nr := &mdNodeRenderer{
		width:                 r.width,
		source:                r.source,
		atBlankLine:           !hasFM, // after frontmatter, emit separator blank line
		oneSpaceAfterSentence: r.oneSpaceAfterSentence,
	}
	gmr := gmrenderer.NewRenderer(
		gmrenderer.WithNodeRenderers(util.Prioritized(nr, 1000)),
	)
	return gmr.Render(w, r.source, r.doc)
}

// mdNodeRenderer implements goldmark's renderer.NodeRenderer interface,
// rendering AST nodes back to formatted Markdown with word wrapping.
type mdNodeRenderer struct {
	width int

	// Rendering state
	source                []byte
	w                     util.BufWriter
	atBlankLine           bool
	oneSpaceAfterSentence bool     // one space after sentence-ending punctuation
	prefixes              []string // prefix stack for nesting

	// funcs stores registered render functions for manual sub-walks.
	funcs map[gmast.NodeKind]gmrenderer.NodeRendererFunc
}

// RegisterFuncs registers render functions for each AST node kind.
func (mr *mdNodeRenderer) RegisterFuncs(reg gmrenderer.NodeRendererFuncRegisterer) {
	mr.funcs = map[gmast.NodeKind]gmrenderer.NodeRendererFunc{}

	// Standard Markdown block nodes.
	mr.register(reg, gmast.KindDocument, mr.renderDocument)
	mr.register(reg, gmast.KindHeading, mr.renderHeading)
	mr.register(reg, gmast.KindParagraph, mr.renderParagraph)
	mr.register(reg, gmast.KindTextBlock, mr.renderParagraph)
	mr.register(reg, gmast.KindThematicBreak, mr.renderThematicBreak)
	mr.register(reg, gmast.KindFencedCodeBlock, mr.renderFencedCodeBlock)
	mr.register(reg, gmast.KindCodeBlock, mr.renderCodeBlock)
	mr.register(reg, gmast.KindBlockquote, mr.renderBlockquote)
	mr.register(reg, gmast.KindList, mr.renderList)
	mr.register(reg, gmast.KindHTMLBlock, mr.renderHTMLBlock)

	// GFM extensions.
	mr.register(reg, gmext.KindTable, mr.renderTable)
	mr.register(reg, gmext.KindFootnoteList, mr.renderFootnoteList)

	// Definition list extension.
	mr.register(reg, gmext.KindDefinitionList, mr.renderDefinitionList)
	mr.register(reg, gmext.KindDefinitionTerm, mr.renderDefinitionTerm)
	mr.register(reg, gmext.KindDefinitionDescription, mr.renderDefinitionDescription)
}

func (mr *mdNodeRenderer) register(
	reg gmrenderer.NodeRendererFuncRegisterer,
	kind gmast.NodeKind,
	f gmrenderer.NodeRendererFunc,
) {
	reg.Register(kind, f)
	mr.funcs[kind] = f
}

// walkNode dispatches a single node (and its subtree) through the
// registered render functions.  Used for manual sub-walks when a parent
// handler returns WalkSkipChildren.
func (mr *mdNodeRenderer) walkNode(node gmast.Node) error {
	return gmast.Walk(node, func(n gmast.Node, entering bool) (gmast.WalkStatus, error) {
		if f := mr.funcs[n.Kind()]; f != nil {
			return f(mr.w, mr.source, n, entering)
		}
		return gmast.WalkContinue, nil
	})
}

// ---------- prefix management ----------

func (mr *mdNodeRenderer) prefix() string {
	return strings.Join(mr.prefixes, "")
}

func (mr *mdNodeRenderer) pushPrefix(p string) {
	mr.prefixes = append(mr.prefixes, p)
}

func (mr *mdNodeRenderer) popPrefix() {
	if len(mr.prefixes) > 0 {
		mr.prefixes = mr.prefixes[:len(mr.prefixes)-1]
	}
}

// ---------- output helpers ----------

func (mr *mdNodeRenderer) emit(s string) error {
	_, err := mr.w.WriteString(s)
	return err
}

func (mr *mdNodeRenderer) blankLine() error {
	if !mr.atBlankLine {
		mr.atBlankLine = true
		return mr.emit("\n")
	}
	return nil
}

// blankLineBefore reports whether the source contains a blank line
// immediately before the line that contains (or starts at) byte offset pos.
// This is used to preserve the original inter-item spacing of lists.
func (mr *mdNodeRenderer) blankLineBefore(pos int) bool {
	// Find the start of the line containing pos.
	lineStart := pos
	for lineStart > 0 && mr.source[lineStart-1] != '\n' {
		lineStart--
	}
	if lineStart == 0 {
		return false // first line of the document
	}
	// lineStart-1 is the '\n' ending the previous line.
	prevEnd := lineStart - 1
	// Find where that previous line begins.
	prevStart := prevEnd
	for prevStart > 0 && mr.source[prevStart-1] != '\n' {
		prevStart--
	}
	// Check whether source[prevStart:prevEnd] is all whitespace (blank line).
	for i := prevStart; i < prevEnd; i++ {
		if mr.source[i] != ' ' && mr.source[i] != '\t' && mr.source[i] != '\r' {
			return false
		}
	}
	return true
}

// ---------- Document ----------

func (mr *mdNodeRenderer) renderDocument(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	mr.w = w // capture writer for helper methods

	// Collect footnote definitions from the AST.
	var footnotes []*gmext.Footnote
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if fl, ok := child.(*gmext.FootnoteList); ok {
			for fc := fl.FirstChild(); fc != nil; fc = fc.NextSibling() {
				if fn, ok := fc.(*gmext.Footnote); ok {
					footnotes = append(footnotes, fn)
				}
			}
		}
	}

	// Find footnote definition source positions.
	fnPositions := mr.footnoteSourcePositions(footnotes)

	// Collect regular (non-FootnoteList) block children with positions.
	type blockEntry struct {
		node gmast.Node
		pos  int
	}
	var blocks []blockEntry
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if _, ok := child.(*gmext.FootnoteList); ok {
			continue // skip; footnotes will be interleaved
		}
		blocks = append(blocks, blockEntry{node: child, pos: mr.blockStartPos(child)})
	}

	// Merge blocks and footnotes in source order.
	renderedFootnotes := map[int]bool{}
	bi, fi := 0, 0
	first := true

	for bi < len(blocks) || fi < len(fnPositions) {
		// Decide whether to emit the next block or the next footnote.
		emitFootnote := false
		if fi < len(fnPositions) {
			if bi >= len(blocks) {
				emitFootnote = true
			} else {
				emitFootnote = fnPositions[fi].pos < blocks[bi].pos
			}
		}

		if emitFootnote {
			fn := fnPositions[fi].fn
			fi++
			if renderedFootnotes[fn.Index] {
				continue
			}
			renderedFootnotes[fn.Index] = true
			if !first {
				if err := mr.blankLine(); err != nil {
					return gmast.WalkStop, err
				}
			}
			first = false
			if err := mr.renderFootnoteInner(fn); err != nil {
				return gmast.WalkStop, err
			}
		} else {
			block := blocks[bi]
			bi++
			if !first {
				if err := mr.blankLine(); err != nil {
					return gmast.WalkStop, err
				}
			}
			first = false
			if err := mr.walkNode(block.node); err != nil {
				return gmast.WalkStop, err
			}
		}
	}

	return gmast.WalkSkipChildren, nil
}

// footnotePos pairs a footnote with its source position.
type footnotePos struct {
	fn  *gmext.Footnote
	pos int
}

// footnoteSourcePositions finds where each footnote definition appears
// in the source by scanning for [^ref]: patterns.
func (mr *mdNodeRenderer) footnoteSourcePositions(footnotes []*gmext.Footnote) []footnotePos {
	if len(footnotes) == 0 {
		return nil
	}

	// Build a map from ref label to footnote.
	refMap := map[string]*gmext.Footnote{}
	for _, fn := range footnotes {
		refMap[string(fn.Ref)] = fn
	}

	// Scan source for [^ref]: patterns, in order.
	re := regexp.MustCompile(`(?m)^\[\^([^\]]+)\]:`)
	matches := re.FindAllSubmatchIndex(mr.source, -1)

	var result []footnotePos
	seen := map[string]bool{}
	for _, m := range matches {
		ref := string(mr.source[m[2]:m[3]])
		if seen[ref] {
			continue
		}
		if fn, ok := refMap[ref]; ok {
			result = append(result, footnotePos{fn: fn, pos: m[0]})
			seen[ref] = true
		}
	}
	return result
}

// blockStartPos returns the source byte offset where a block node begins.
func (mr *mdNodeRenderer) blockStartPos(n gmast.Node) int {
	if n.Type() != gmast.TypeInline {
		if lines := n.Lines(); lines != nil && lines.Len() > 0 {
			return lines.At(0).Start
		}
	}
	if c := n.FirstChild(); c != nil {
		return mr.blockStartPos(c)
	}
	return 0
}

// ---------- Heading ----------

func (mr *mdNodeRenderer) renderHeading(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	heading := n.(*gmast.Heading)
	hashes := strings.Repeat("#", heading.Level)
	text := mr.inlineText(n)
	if err := mr.emit(mr.prefix() + hashes + " " + text + "\n"); err != nil {
		return gmast.WalkStop, err
	}
	return gmast.WalkSkipChildren, nil
}

// ---------- Paragraph / TextBlock ----------

func (mr *mdNodeRenderer) renderParagraph(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	frags := mr.inlineFragments(n)
	if err := mr.emitWrapped(frags, mr.prefix()); err != nil {
		return gmast.WalkStop, err
	}
	return gmast.WalkSkipChildren, nil
}

// ---------- ThematicBreak ----------

func (mr *mdNodeRenderer) renderThematicBreak(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	if err := mr.emit(mr.prefix() + "---\n"); err != nil {
		return gmast.WalkStop, err
	}
	return gmast.WalkContinue, nil
}

// ---------- FencedCodeBlock ----------

func (mr *mdNodeRenderer) renderFencedCodeBlock(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	fcb := n.(*gmast.FencedCodeBlock)
	p := mr.prefix()
	lang := ""
	if fcb.Info != nil {
		lang = string(fcb.Info.Value(source))
		if idx := strings.IndexByte(lang, ' '); idx >= 0 {
			lang = lang[:idx]
		}
	}
	if err := mr.emit(p + "```" + lang + "\n"); err != nil {
		return gmast.WalkStop, err
	}
	lines := fcb.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if err := mr.emit(p + string(seg.Value(source))); err != nil {
			return gmast.WalkStop, err
		}
	}
	if err := mr.emit(p + "```\n"); err != nil {
		return gmast.WalkStop, err
	}
	return gmast.WalkSkipChildren, nil
}

// ---------- CodeBlock (indented) ----------

func (mr *mdNodeRenderer) renderCodeBlock(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	cb := n.(*gmast.CodeBlock)
	p := mr.prefix()
	lines := cb.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if err := mr.emit(p + "    " + string(seg.Value(source))); err != nil {
			return gmast.WalkStop, err
		}
	}
	return gmast.WalkSkipChildren, nil
}

// ---------- Blockquote ----------
// Uses WalkContinue so the framework walks children automatically;
// we just push/pop the "> " prefix.

func (mr *mdNodeRenderer) renderBlockquote(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if entering {
		if err := mr.blankLine(); err != nil {
			return gmast.WalkStop, err
		}
		mr.pushPrefix("> ")
		mr.atBlankLine = true // suppress blank line before first child
	} else {
		mr.popPrefix()
	}
	return gmast.WalkContinue, nil
}

// ---------- List ----------
// Uses WalkSkipChildren because bullet formatting, tight/loose spacing,
// and continuation wrapping require manual control over child traversal.

func (mr *mdNodeRenderer) renderList(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false

	list := n.(*gmast.List)
	itemNum := list.Start
	if itemNum == 0 {
		itemNum = 1
	}

	first := true
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if _, ok := child.(*gmast.ListItem); !ok {
			continue
		}

		if !first && mr.blankLineBefore(mr.blockStartPos(child)) {
			if err := mr.blankLine(); err != nil {
				return gmast.WalkStop, err
			}
		}
		first = false

		var bullet string
		if list.IsOrdered() {
			bullet = fmt.Sprintf("%d%c ", itemNum, list.Marker)
			itemNum++
		} else {
			bullet = string(list.Marker) + " "
		}

		indent := strings.Repeat(" ", len(bullet))

		firstChild := true
		for itemChild := child.FirstChild(); itemChild != nil; itemChild = itemChild.NextSibling() {
			if !firstChild {
				if err := mr.blankLine(); err != nil {
					return gmast.WalkStop, err
				}
			}

			if firstChild {
				if err := mr.emit(mr.prefix() + bullet); err != nil {
					return gmast.WalkStop, err
				}
				mr.pushPrefix(indent)
				if err := mr.renderListItemFirstChild(itemChild); err != nil {
					return gmast.WalkStop, err
				}
			} else {
				if err := mr.walkNode(itemChild); err != nil {
					return gmast.WalkStop, err
				}
			}
			firstChild = false
		}

		if !firstChild {
			mr.popPrefix()
		}
	}
	return gmast.WalkSkipChildren, nil
}

// renderListItemFirstChild renders the first block of a list item,
// where the bullet has already been emitted on the current line.
func (mr *mdNodeRenderer) renderListItemFirstChild(node gmast.Node) error {
	mr.atBlankLine = false
	switch node.(type) {
	case *gmast.Paragraph, *gmast.TextBlock:
		frags := mr.inlineFragments(node)
		return mr.emitWrappedContinuation(frags, mr.prefix())
	default:
		if err := mr.emit("\n"); err != nil {
			return err
		}
		mr.atBlankLine = true // suppress child's leading blankLine()
		return mr.walkNode(node)
	}
}

// ---------- DefinitionList ----------
// Uses WalkContinue: the list is a container whose DefinitionTerm and
// DefinitionDescription children are each handled by their own renderers.

func (mr *mdNodeRenderer) renderDefinitionList(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if entering {
		if err := mr.blankLine(); err != nil {
			return gmast.WalkStop, err
		}
		mr.atBlankLine = true // suppress blank line before first child
	}
	return gmast.WalkContinue, nil
}

// renderDefinitionTerm renders the term (the word being defined).
// Terms contain inline content and are emitted as a standalone line.
func (mr *mdNodeRenderer) renderDefinitionTerm(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	// Blank line between successive term/description groups.
	// The first term in the list gets its blank line from renderDefinitionList.
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	frags := mr.inlineFragments(n)
	if err := mr.emitWrapped(frags, mr.prefix()); err != nil {
		return gmast.WalkStop, err
	}
	return gmast.WalkSkipChildren, nil
}

// renderDefinitionDescription renders a definition (`: ` prefixed description).
// Each description is treated like a list item: the first block gets
// emitted inline after the `: ` marker, subsequent blocks are indented.
func (mr *mdNodeRenderer) renderDefinitionDescription(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	mr.atBlankLine = false

	const marker = ": "
	const indent = "  "

	firstChild := true
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if firstChild {
			if err := mr.emit(mr.prefix() + marker); err != nil {
				return gmast.WalkStop, err
			}
			mr.pushPrefix(indent)
			if err := mr.renderListItemFirstChild(child); err != nil {
				return gmast.WalkStop, err
			}
		} else {
			if err := mr.blankLine(); err != nil {
				return gmast.WalkStop, err
			}
			if err := mr.walkNode(child); err != nil {
				return gmast.WalkStop, err
			}
		}
		firstChild = false
	}

	if !firstChild {
		mr.popPrefix()
	}
	return gmast.WalkSkipChildren, nil
}

// ---------- HTMLBlock ----------

func (mr *mdNodeRenderer) renderHTMLBlock(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false
	p := mr.prefix()
	htmlBlock := n.(*gmast.HTMLBlock)
	lines := htmlBlock.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if err := mr.emit(p + string(seg.Value(source))); err != nil {
			return gmast.WalkStop, err
		}
	}
	if htmlBlock.HasClosure() {
		seg := htmlBlock.ClosureLine
		if err := mr.emit(p + string(seg.Value(source))); err != nil {
			return gmast.WalkStop, err
		}
	}
	return gmast.WalkSkipChildren, nil
}

// ---------- Table (GFM) ----------

func (mr *mdNodeRenderer) renderTable(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	if !entering {
		return gmast.WalkContinue, nil
	}
	if err := mr.blankLine(); err != nil {
		return gmast.WalkStop, err
	}
	mr.atBlankLine = false

	table := n.(*gmext.Table)
	p := mr.prefix()

	// Collect all rows (header + body).
	var rows []gmast.Node
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		rows = append(rows, child)
	}

	// Compute column widths by measuring rendered cell content.
	numCols := len(table.Alignments)
	colWidths := make([]int, numCols)
	var allCells [][]string
	for _, row := range rows {
		var cells []string
		col := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			text := mr.inlineText(cell)
			cells = append(cells, text)
			if col < numCols && len(text) > colWidths[col] {
				colWidths[col] = len(text)
			}
			col++
		}
		allCells = append(allCells, cells)
	}

	// Minimum column width is 3 (for separator like ---).
	for i := range colWidths {
		if colWidths[i] < 3 {
			colWidths[i] = 3
		}
	}

	// Emit header row.
	if len(allCells) > 0 {
		if err := mr.emitTableRow(allCells[0], colWidths, table.Alignments, p); err != nil {
			return gmast.WalkStop, err
		}
	}

	// Emit separator row.
	if err := mr.emitTableSeparator(table.Alignments, colWidths, p); err != nil {
		return gmast.WalkStop, err
	}

	// Emit body rows.
	for _, cells := range allCells[1:] {
		if err := mr.emitTableRow(cells, colWidths, table.Alignments, p); err != nil {
			return gmast.WalkStop, err
		}
	}

	return gmast.WalkSkipChildren, nil
}

func (mr *mdNodeRenderer) emitTableRow(
	cells []string, colWidths []int, alignments []gmext.Alignment, p string,
) error {
	if err := mr.emit(p + "|"); err != nil {
		return err
	}
	for i, w := range colWidths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		align := gmext.AlignNone
		if i < len(alignments) {
			align = alignments[i]
		}
		pad := w - len(cell)
		var padded string
		switch align {
		case gmext.AlignRight:
			padded = strings.Repeat(" ", pad) + cell
		case gmext.AlignCenter:
			left := pad / 2
			right := pad - left
			padded = strings.Repeat(" ", left) + cell + strings.Repeat(" ", right)
		default:
			padded = cell + strings.Repeat(" ", pad)
		}
		if err := mr.emit(" " + padded + " |"); err != nil {
			return err
		}
	}
	return mr.emit("\n")
}

func (mr *mdNodeRenderer) emitTableSeparator(
	alignments []gmext.Alignment, colWidths []int, p string,
) error {
	if err := mr.emit(p + "|"); err != nil {
		return err
	}
	for i, w := range colWidths {
		align := gmext.AlignNone
		if i < len(alignments) {
			align = alignments[i]
		}
		// Fill the full column slot (w+2) with dashes, Emacs-style.
		fw := w + 2
		var sep string
		switch align {
		case gmext.AlignLeft:
			sep = ":" + strings.Repeat("-", fw-1)
		case gmext.AlignRight:
			sep = strings.Repeat("-", fw-1) + ":"
		case gmext.AlignCenter:
			sep = ":" + strings.Repeat("-", fw-2) + ":"
		default:
			sep = strings.Repeat("-", fw)
		}
		if err := mr.emit(sep + "|"); err != nil {
			return err
		}
	}
	return mr.emit("\n")
}

// ---------- FootnoteList (GFM) ----------
// Footnotes are rendered by renderDocument at their original source
// positions.  This handler is a no-op that skips the FootnoteList
// in the normal walk.

func (mr *mdNodeRenderer) renderFootnoteList(
	w util.BufWriter, source []byte, n gmast.Node, entering bool,
) (gmast.WalkStatus, error) {
	return gmast.WalkSkipChildren, nil
}

// renderFootnoteInner renders a single footnote definition.
func (mr *mdNodeRenderer) renderFootnoteInner(fn *gmext.Footnote) error {
	mr.atBlankLine = false
	label := fmt.Sprintf("[^%s]: ", fn.Ref)
	indent := strings.Repeat(" ", len(label))

	if err := mr.emit(mr.prefix() + label); err != nil {
		return err
	}
	mr.pushPrefix(indent)
	defer mr.popPrefix()

	firstChild := true
	for child := fn.FirstChild(); child != nil; child = child.NextSibling() {
		if !firstChild {
			if err := mr.blankLine(); err != nil {
				return err
			}
		}

		if firstChild {
			switch child.(type) {
			case *gmast.Paragraph, *gmast.TextBlock:
				frags := mr.inlineFragments(child)
				if err := mr.emitWrappedContinuation(frags, mr.prefix()); err != nil {
					return err
				}
			default:
				if err := mr.emit("\n"); err != nil {
					return err
				}
				mr.atBlankLine = true
				if err := mr.walkNode(child); err != nil {
					return err
				}
			}
		} else {
			if err := mr.walkNode(child); err != nil {
				return err
			}
		}
		firstChild = false
	}
	return nil
}

// ========== Inline fragment collection ==========

// inlineFragment represents a piece of inline content (typically a word
// or marked-up unit).
type inlineFragment struct {
	text        string
	hardBreak   bool // force a line break after this fragment
	spacesAfter int  // spaces after this fragment in source (-1 = unknown, use default)
}

// inlineFragments collects the inline content of a block node into fragments.
func (mr *mdNodeRenderer) inlineFragments(node gmast.Node) []inlineFragment {
	var frags []inlineFragment
	mr.collectInlineFragments(node, &frags)
	return frags
}

// collectInlineFragments walks inline children and accumulates fragments.
func (mr *mdNodeRenderer) collectInlineFragments(node gmast.Node, frags *[]inlineFragment) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *gmast.Text:
			val := string(n.Value(mr.source))
			words, spacings, trailingSpaces := parseWordsWithSpacing(val)
			// Goldmark sometimes splits text at multi-space
			// positions, distributing the whitespace across two
			// Text nodes (e.g. "process. " + " Some" for a
			// source gap of two spaces).  When this node starts
			// with leading whitespace, combine it with the
			// previous fragment's trailing space count to recover
			// the original gap width.
			leadingSpaces := 0
			for leadingSpaces < len(val) && (val[leadingSpaces] == ' ' || val[leadingSpaces] == '\t') {
				leadingSpaces++
			}
			if leadingSpaces > 0 && len(*frags) > 0 {
				prev := &(*frags)[len(*frags)-1]
				prevTrailing := prev.spacesAfter
				if prevTrailing < 0 {
					prevTrailing = 0
				}
				prev.spacesAfter = min(prevTrailing+leadingSpaces, 2)
			}
			// If the raw text has no leading whitespace AND the
			// previous sibling is an inline markup node OR a Text
			// node with no trailing whitespace, glue the first word
			// to the previous fragment.  This handles punctuation
			// after inline markup (e.g. "[x](url)." where "." is a
			// separate Text node) and non-link brackets that
			// Goldmark splits into separate Text nodes (e.g.
			// ".  [" followed by "This").
			glue := len(val) > 0 && !unicode.IsSpace(rune(val[0])) &&
				(mr.prevIsMarkup(child) || mr.prevTextHasNoTrailingSpace(child))
			for i, w := range words {
				// Determine spacesAfter for this word.
				sa := -1
				if i < len(spacings) {
					sa = spacings[i]
				} else if trailingSpaces > 0 {
					sa = min(trailingSpaces, 2)
				}
				if i == 0 && glue && len(*frags) > 0 {
					// Append to the previous fragment.
					(*frags)[len(*frags)-1].text += w
					(*frags)[len(*frags)-1].spacesAfter = sa
				} else {
					*frags = append(*frags, inlineFragment{text: w, spacesAfter: sa})
				}
			}
			if n.HardLineBreak() {
				if len(*frags) > 0 {
					(*frags)[len(*frags)-1].hardBreak = true
				}
			}
		case *gmast.CodeSpan:
			code := mr.collectRawText(n)
			mr.addInlineFrag(frags, child, "`"+code+"`")
		case *gmast.Emphasis:
			inner := mr.collectInlineString(n)
			marker := "*"
			if n.Level == 2 {
				marker = "**"
			}
			mr.addInlineFrag(frags, child, marker+inner+marker)
		case *gmast.Link:
			dest := string(n.Destination)
			title := string(n.Title)
			suffix := "](" + dest
			if title != "" {
				suffix += " \"" + title + "\""
			}
			suffix += ")"
			mr.addBreakableMarkupFrags(frags, child, "[", suffix)
		case *gmast.Image:
			dest := string(n.Destination)
			title := string(n.Title)
			suffix := "](" + dest
			if title != "" {
				suffix += " \"" + title + "\""
			}
			suffix += ")"
			mr.addBreakableMarkupFrags(frags, child, "![", suffix)
		case *gmast.AutoLink:
			url := string(n.URL(mr.source))
			mr.addInlineFrag(frags, child, "<"+url+">")
		case *gmast.RawHTML:
			html := mr.segmentsText(n)
			mr.addInlineFrag(frags, child, html)
		case *gmast.String:
			*frags = append(*frags, inlineFragment{text: string(n.Value)})
		case *gmext.Strikethrough:
			inner := mr.collectInlineString(n)
			mr.addInlineFrag(frags, child, "~~"+inner+"~~")
		case *gmext.TaskCheckBox:
			if n.IsChecked {
				*frags = append(*frags, inlineFragment{text: "[x]"})
			} else {
				*frags = append(*frags, inlineFragment{text: "[ ]"})
			}
		case *gmext.FootnoteLink:
			mr.addInlineFrag(frags, child, fmt.Sprintf("[^%d]", n.Index))
		case *gmext.FootnoteBacklink:
			// Skip backlinks in rendering (they are generated).
		default:
			mr.collectInlineFragments(child, frags)
		}
	}
}

// prevIsMarkup returns true if the previous sibling is an inline markup
// node (not a plain Text node).
func (mr *mdNodeRenderer) prevIsMarkup(n gmast.Node) bool {
	ps := n.PreviousSibling()
	if ps == nil {
		return false
	}
	switch ps.(type) {
	case *gmast.Emphasis, *gmast.Link, *gmast.Image,
		*gmast.CodeSpan, *gmast.AutoLink, *gmast.RawHTML,
		*gmext.Strikethrough, *gmext.FootnoteLink:
		return true
	}
	return false
}

// prevTextHasNoTrailingSpace returns true if the previous sibling is a
// Text node whose value does not end with whitespace, meaning there is
// no gap between the previous text and this inline node.
// We check two things:
//  1. The Text node value has no trailing whitespace (Goldmark preserves
//     inline spaces in the value).
//  2. The source byte immediately after the Text segment is not
//     whitespace (catches newlines that Goldmark strips from values).
func (mr *mdNodeRenderer) prevTextHasNoTrailingSpace(n gmast.Node) bool {
	ps := n.PreviousSibling()
	if ps == nil {
		return false
	}
	t, ok := ps.(*gmast.Text)
	if !ok {
		return false
	}
	// Check the node value for trailing space.
	val := t.Value(mr.source)
	if len(val) > 0 && unicode.IsSpace(rune(val[len(val)-1])) {
		return false
	}
	// Check the source for whitespace after the segment (catches newlines).
	end := t.Segment.Stop
	if end < len(mr.source) && unicode.IsSpace(rune(mr.source[end])) {
		return false
	}
	return len(val) > 0
}

// addInlineFrag adds a markup fragment, gluing it to the previous fragment
// if the previous sibling Text node has no trailing whitespace (meaning
// the markup immediately follows text with no space, e.g. "word[^1]"),
// or if the previous sibling is another markup node (e.g. a link
// immediately followed by a footnote: "[text](url)[^1]").
func (mr *mdNodeRenderer) addInlineFrag(frags *[]inlineFragment, node gmast.Node, text string) {
	if (mr.prevTextHasNoTrailingSpace(node) || mr.prevIsMarkup(node)) && len(*frags) > 0 {
		(*frags)[len(*frags)-1].text += text
		(*frags)[len(*frags)-1].spacesAfter = -1
	} else {
		*frags = append(*frags, inlineFragment{text: text, spacesAfter: -1})
	}
}

// addBreakableMarkupFrags adds fragments for a link or image whose inner
// text can be broken across lines.  The prefix ("[" or "![") is prepended
// to the first inner word, and the suffix ("](url)") is appended to the
// last inner word.  If there are multiple inner words, intermediate ones
// become independent fragments that allow line breaks.
func (mr *mdNodeRenderer) addBreakableMarkupFrags(
	frags *[]inlineFragment, node gmast.Node, prefix, suffix string,
) {
	innerFrags := mr.inlineFragments(node)
	if len(innerFrags) == 0 {
		// Empty content: [](url) or ![](url)
		mr.addInlineFrag(frags, node, prefix+suffix)
		return
	}
	innerFrags[0].text = prefix + innerFrags[0].text
	innerFrags[len(innerFrags)-1].text += suffix
	innerFrags[len(innerFrags)-1].spacesAfter = -1 // unknown: depends on outer context
	// Glue the first inner fragment to the previous fragment when
	// the link immediately follows text with no whitespace.
	if mr.prevTextHasNoTrailingSpace(node) && len(*frags) > 0 {
		(*frags)[len(*frags)-1].text += innerFrags[0].text
		(*frags)[len(*frags)-1].spacesAfter = innerFrags[0].spacesAfter
		*frags = append(*frags, innerFrags[1:]...)
	} else {
		*frags = append(*frags, innerFrags...)
	}
}

// collectRawText returns the raw text content of an inline node's children.
func (mr *mdNodeRenderer) collectRawText(node gmast.Node) string {
	var sb strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*gmast.Text); ok {
			sb.Write(t.Value(mr.source))
		}
	}
	return sb.String()
}

// collectInlineString collects inline content as a single string,
// preserving markup.
func (mr *mdNodeRenderer) collectInlineString(node gmast.Node) string {
	var frags []inlineFragment
	mr.collectInlineFragments(node, &frags)
	var parts []string
	for _, f := range frags {
		parts = append(parts, f.text)
	}
	return strings.Join(parts, " ")
}

// segmentsText returns the source text for all line segments of a node.
func (mr *mdNodeRenderer) segmentsText(node gmast.Node) string {
	var sb strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(mr.source))
	}
	return sb.String()
}

// inlineText returns the rendered inline text of a block node as a single
// string (no wrapping).
func (mr *mdNodeRenderer) inlineText(node gmast.Node) string {
	frags := mr.inlineFragments(node)
	var parts []string
	for _, f := range frags {
		parts = append(parts, f.text)
	}
	return strings.Join(parts, " ")
}

// ========== Word wrapping ==========

// endsWithSentence returns true if the fragment text ends with
// sentence-ending punctuation (. ? !), optionally followed by
// closing quotes, parentheses, or brackets.
func endsWithSentence(s string) bool {
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case '.', '?', '!':
			return true
		case '"', '\'', ')', ']', '`':
			continue // skip trailing closers
		default:
			return false
		}
	}
	return false
}

// sentenceBreak returns the default spacing to use between two fragments
// when the original source spacing is unknown (e.g. at a line boundary).
// It returns "  " (double space) when the previous fragment ends a
// sentence, unless oneSpaceAfterSentence is set.
func sentenceBreak(prev string, oneSpaceAfterSentence bool) string {
	if !oneSpaceAfterSentence && endsWithSentence(prev) && !looksLikeInitial(prev) {
		return "  "
	}
	return " "
}

// looksLikeInitial returns true if the text looks like a personal initial
// such as "J." or "**J.**" — a single letter followed by a period,
// possibly surrounded by markup characters and trailing closers.
// Single-letter initials are probably not sentence-ending.
func looksLikeInitial(s string) bool {
	// Strip trailing closers and markup.
	end := len(s)
	for end > 0 {
		switch s[end-1] {
		case '"', '\'', ')', ']', '`', '*', '_', '~':
			end--
			continue
		}
		break
	}
	// Must end with a period.
	if end == 0 || s[end-1] != '.' {
		return false
	}
	end--
	// Strip leading markup.
	start := 0
	for start < end {
		switch s[start] {
		case '*', '_', '~', '`', '[', '(':
			start++
			continue
		}
		break
	}
	// What remains must be a single letter.
	if end-start != 1 {
		return false
	}
	r := rune(s[start])
	return unicode.IsLetter(r)
}

// parseWordsWithSpacing splits a string into words and records inter-word
// spacing.  spacings[i] is the number of spaces between words[i] and
// words[i+1], capped at 2.  trailingSpaces is the number of spaces
// after the last word, capped at 2.
func parseWordsWithSpacing(s string) (words []string, spacings []int, trailingSpaces int) {
	i := 0
	// Skip leading whitespace.
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for i < len(s) {
		// Find end of word.
		j := i
		for j < len(s) && s[j] != ' ' && s[j] != '\t' {
			j++
		}
		words = append(words, s[i:j])
		// Count trailing spaces.
		k := j
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		if k < len(s) {
			// Another word follows; record gap capped at 2.
			spacings = append(spacings, min(k-j, 2))
		} else {
			// End of string; record trailing spaces.
			trailingSpaces = min(k-j, 2)
		}
		i = k
	}
	return
}

// emitWrapped writes fragments word-wrapped at the configured width.
func (mr *mdNodeRenderer) emitWrapped(fragments []inlineFragment, p string) error {
	if len(fragments) == 0 {
		return nil
	}

	col := len(p)
	if err := mr.emit(p); err != nil {
		return err
	}

	startOfLine := true
	var prevFrag inlineFragment
	for _, frag := range fragments {
		wordLen := len(frag.text)

		if startOfLine {
			if err := mr.emit(frag.text); err != nil {
				return err
			}
			col += wordLen
			startOfLine = false
		} else {
			sp := mr.spacingAfter(prevFrag)
			if col+len(sp)+wordLen <= mr.width {
				if err := mr.emit(sp + frag.text); err != nil {
					return err
				}
				col += len(sp) + wordLen
			} else {
				if err := mr.emit("\n" + p + frag.text); err != nil {
					return err
				}
				col = len(p) + wordLen
			}
		}

		prevFrag = frag

		if frag.hardBreak {
			if err := mr.emit("  \n" + p); err != nil {
				return err
			}
			col = len(p)
			startOfLine = true
			prevFrag = inlineFragment{}
		}
	}

	return mr.emit("\n")
}

// spacingAfter returns the spacing string to emit after a fragment.
// If the fragment has a known spacesAfter from the original source,
// that is preserved; otherwise the default sentence-break heuristic
// is used.
func (mr *mdNodeRenderer) spacingAfter(prev inlineFragment) string {
	if prev.spacesAfter >= 1 {
		if prev.spacesAfter >= 2 {
			return "  "
		}
		return " "
	}
	// Unknown spacing (line boundary or markup): use flag-based default.
	return sentenceBreak(prev.text, mr.oneSpaceAfterSentence)
}

// emitWrappedContinuation is like emitWrapped but assumes the first line's
// prefix has already been emitted (used after a bullet or footnote label).
func (mr *mdNodeRenderer) emitWrappedContinuation(fragments []inlineFragment, p string) error {
	if len(fragments) == 0 {
		return mr.emit("\n")
	}

	col := len(p)
	startOfLine := true
	var prevFrag inlineFragment

	for _, frag := range fragments {
		wordLen := len(frag.text)

		if startOfLine {
			if err := mr.emit(frag.text); err != nil {
				return err
			}
			col += wordLen
			startOfLine = false
		} else {
			sp := mr.spacingAfter(prevFrag)
			if col+len(sp)+wordLen <= mr.width {
				if err := mr.emit(sp + frag.text); err != nil {
					return err
				}
				col += len(sp) + wordLen
			} else {
				if err := mr.emit("\n" + p + frag.text); err != nil {
					return err
				}
				col = len(p) + wordLen
			}
		}

		prevFrag = frag

		if frag.hardBreak {
			if err := mr.emit("  \n" + p); err != nil {
				return err
			}
			col = len(p)
			startOfLine = true
			prevFrag = inlineFragment{}
		}
	}

	return mr.emit("\n")
}
