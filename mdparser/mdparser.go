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

	"github.com/ts4z/mdindent/mdio"
	"github.com/yuin/goldmark"
	gmast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gmext "github.com/yuin/goldmark/extension/ast"
	gmrenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Parser parses Markdown source into a Renderable using Goldmark.
type Parser struct{}

// Parse parses source into a renderable Markdown document.
func (p *Parser) Parse(source []byte) (mdio.Renderable, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
		),
	)
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)
	return &renderable{doc: doc, source: source}, nil
}

// renderable holds a parsed Goldmark AST and the original source,
// and can render the AST back to Markdown.
type renderable struct {
	doc    gmast.Node
	source []byte
}

// Render writes reformatted Markdown to w, wrapping paragraphs at width.
// It creates a fresh Goldmark renderer with our NodeRenderer for each call.
func (r *renderable) Render(w io.Writer, opts mdio.RenderOptions) error {
	nr := &mdNodeRenderer{
		width:                  opts.Width,
		source:                 r.source,
		atBlankLine:            true, // suppress blank line before first block
		twoSpacesAfterSentence: opts.TwoSpacesAfterSentence,
	}
	gmr := gmrenderer.NewRenderer(
		gmrenderer.WithNodeRenderers(util.Prioritized(nr, 1000)),
	)
	return gmr.Render(w, r.source, r.doc)
}

// mdNodeRenderer implements goldmark's renderer.NodeRenderer interface,
// rendering AST nodes back to formatted Markdown with word wrapping.
type mdNodeRenderer struct {
	width                  int
	source                 []byte
	w                      util.BufWriter
	atBlankLine            bool
	twoSpacesAfterSentence bool     // use two spaces after sentence-ending punctuation
	prefixes               []string // prefix stack for nesting

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

		if !first && !list.IsTight {
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
		var sep string
		switch align {
		case gmext.AlignLeft:
			sep = ":" + strings.Repeat("-", w-1)
		case gmext.AlignRight:
			sep = strings.Repeat("-", w-1) + ":"
		case gmext.AlignCenter:
			sep = ":" + strings.Repeat("-", w-2) + ":"
		default:
			sep = strings.Repeat("-", w)
		}
		if err := mr.emit(" " + sep + " |"); err != nil {
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
	text      string
	hardBreak bool // force a line break after this fragment
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
			words := strings.Fields(val)
			// If the raw text has no leading whitespace AND the
			// previous sibling is an inline markup node, glue the
			// first word to the previous fragment.  This handles
			// punctuation after inline markup (e.g. "[x](url)."
			// where "." is a separate Text node) without
			// incorrectly merging words across line continuations
			// (where consecutive Text nodes represent source lines).
			glue := len(val) > 0 && !unicode.IsSpace(rune(val[0])) && mr.prevIsMarkup(child)
			for i, w := range words {
				if i == 0 && glue && len(*frags) > 0 {
					// Append to the previous fragment.
					(*frags)[len(*frags)-1].text += w
				} else {
					*frags = append(*frags, inlineFragment{text: w})
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
// the markup immediately follows text with no space, e.g. "word[^1]").
func (mr *mdNodeRenderer) addInlineFrag(frags *[]inlineFragment, node gmast.Node, text string) {
	if mr.prevTextHasNoTrailingSpace(node) && len(*frags) > 0 {
		(*frags)[len(*frags)-1].text += text
	} else {
		*frags = append(*frags, inlineFragment{text: text})
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
	// Glue the first inner fragment to the previous fragment when
	// the link immediately follows text with no whitespace.
	if mr.prevTextHasNoTrailingSpace(node) && len(*frags) > 0 {
		(*frags)[len(*frags)-1].text += innerFrags[0].text
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

// neverEndsSentence is the set of abbreviations (lowercased) that are
// never followed by a sentence break, even though they end with a period.
var neverEndsSentence = map[string]bool{
	"i.e.": true, "e.g.": true, "cf.": true, "vs.": true,
	"viz.": true, "al.": true, "approx.": true, "dept.": true,
	"est.": true, "govt.": true, "no.": true,
}

// isKnownAbbreviation returns true if word (ignoring any trailing
// closers like quotes or parens) is a known abbreviation that never
// ends a sentence.
func isKnownAbbreviation(s string) bool {
	// Strip trailing closers to find the core word.
	end := len(s)
	for end > 0 {
		switch s[end-1] {
		case '"', '\'', ')', ']', '`':
			end--
			continue
		}
		break
	}
	// Strip leading markup to find the core word.
	start := 0
	for start < end {
		switch s[start] {
		case '*', '_', '~', '`', '[', '(':
			start++
			continue
		}
		break
	}
	return neverEndsSentence[strings.ToLower(s[start:end])]
}

// startsWithUpper returns true if the first letter of s is uppercase.
// Returns false for empty strings or strings starting with non-letters.
func startsWithUpper(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return unicode.IsUpper(r)
		}
		// Skip leading markup characters like [, *, !, ~
		switch r {
		case '[', '(', '*', '_', '~', '`', '!':
			continue
		}
		return false
	}
	return false
}

// sentenceBreak returns the spacing to use between two fragments.
// It returns "  " (double space) when the previous fragment ends a
// sentence and the next fragment starts a new one.
func sentenceBreak(prev, next string, twoSpacesAfterSentence bool) string {
	if twoSpacesAfterSentence && endsWithSentence(prev) && !isKnownAbbreviation(prev) && startsWithUpper(next) {
		return "  "
	}
	return " "
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
	prevText := ""
	for _, frag := range fragments {
		wordLen := len(frag.text)

		if startOfLine {
			if err := mr.emit(frag.text); err != nil {
				return err
			}
			col += wordLen
			startOfLine = false
		} else {
			sp := sentenceBreak(prevText, frag.text, mr.twoSpacesAfterSentence)
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

		prevText = frag.text

		if frag.hardBreak {
			if err := mr.emit("  \n" + p); err != nil {
				return err
			}
			col = len(p)
			startOfLine = true
			prevText = ""
		}
	}

	return mr.emit("\n")
}

// emitWrappedContinuation is like emitWrapped but assumes the first line's
// prefix has already been emitted (used after a bullet or footnote label).
func (mr *mdNodeRenderer) emitWrappedContinuation(fragments []inlineFragment, p string) error {
	if len(fragments) == 0 {
		return mr.emit("\n")
	}

	col := len(p)
	startOfLine := true
	prevText := ""

	for _, frag := range fragments {
		wordLen := len(frag.text)

		if startOfLine {
			if err := mr.emit(frag.text); err != nil {
				return err
			}
			col += wordLen
			startOfLine = false
		} else {
			sp := sentenceBreak(prevText, frag.text, mr.twoSpacesAfterSentence)
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

		prevText = frag.text

		if frag.hardBreak {
			if err := mr.emit("  \n" + p); err != nil {
				return err
			}
			col = len(p)
			startOfLine = true
			prevText = ""
		}
	}

	return mr.emit("\n")
}
