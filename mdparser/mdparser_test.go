package mdparser

import (
	"bytes"
	"testing"

	"github.com/ts4z/mdindent/mdio"
)

// roundTrip parses source with width and renders back to a string.
func roundTrip(t *testing.T, source string, width int) string {
	t.Helper()
	return roundTripOpts(t, source, mdio.RenderOptions{Width: width})
}

// roundTripOpts parses source and renders with the given options.
func roundTripOpts(t *testing.T, source string, opts mdio.RenderOptions) string {
	t.Helper()
	p := &Parser{}
	r, err := p.Parse([]byte(source))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	var buf bytes.Buffer
	if err := r.Render(&buf, opts); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	return buf.String()
}

func TestEmptyInput(t *testing.T) {
	got := roundTrip(t, "", 79)
	if got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func TestHeading(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"h1", "# Hello\n", "# Hello\n"},
		{"h2", "## World\n", "## World\n"},
		{"h3", "### Three\n", "### Three\n"},
		{"h1 no trailing newline", "# Hello", "# Hello\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundTrip(t, tt.input, 79)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParagraph(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{
			name:  "short paragraph unchanged",
			input: "Hello world.\n",
			width: 79,
			want:  "Hello world.\n",
		},
		{
			name:  "long paragraph wraps",
			input: "The quick brown fox jumps over the lazy dog and keeps on running further away.\n",
			width: 40,
			want:  "The quick brown fox jumps over the lazy\ndog and keeps on running further away.\n",
		},
		{
			name:  "very narrow wrap",
			input: "one two three four five\n",
			width: 10,
			want:  "one two\nthree four\nfive\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundTrip(t, tt.input, tt.width)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTwoParagraphs(t *testing.T) {
	input := "First paragraph.\n\nSecond paragraph.\n"
	want := "First paragraph.\n\nSecond paragraph.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestThematicBreak(t *testing.T) {
	input := "Above.\n\n---\n\nBelow.\n"
	want := "Above.\n\n---\n\nBelow.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFencedCodeBlock(t *testing.T) {
	input := "```go\nfunc main() {}\n```\n"
	want := "```go\nfunc main() {}\n```\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBlockquote(t *testing.T) {
	input := "> This is a quote.\n"
	want := "> This is a quote.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnorderedList(t *testing.T) {
	input := "- one\n- two\n- three\n"
	want := "- one\n- two\n- three\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrderedList(t *testing.T) {
	input := "1. first\n2. second\n3. third\n"
	want := "1. first\n2. second\n3. third\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEmphasis(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"italic", "This is *italic* text.\n", "This is *italic* text.\n"},
		{"bold", "This is **bold** text.\n", "This is **bold** text.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundTrip(t, tt.input, 79)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodeSpan(t *testing.T) {
	input := "Use the `fmt` package.\n"
	want := "Use the `fmt` package.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLink(t *testing.T) {
	input := "See [example](https://example.com) for details.\n"
	want := "See [example](https://example.com) for details.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImage(t *testing.T) {
	input := "![alt text](image.png)\n"
	want := "![alt text](image.png)\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Negative / edge cases

func TestWordLongerThanWidth(t *testing.T) {
	// A single word longer than width can't be broken; it should appear on
	// its own line without being mangled.
	input := "supercalifragilisticexpialidocious\n"
	want := "supercalifragilisticexpialidocious\n"
	got := roundTrip(t, input, 10)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOnlyWhitespace(t *testing.T) {
	got := roundTrip(t, "   \n\n  \n", 79)
	if got != "" {
		t.Errorf("expected empty output for whitespace-only input, got %q", got)
	}
}

func TestHardLineBreak(t *testing.T) {
	// Two trailing spaces = hard line break in CommonMark.
	input := "line one  \nline two\n"
	want := "line one  \nline two\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNestedBlockquote(t *testing.T) {
	input := "> > Nested quote.\n"
	want := "> > Nested quote.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBlockquoteWraps(t *testing.T) {
	input := "> The quick brown fox jumps over the lazy dog and runs away.\n"
	want := "> The quick brown fox jumps over\n> the lazy dog and runs away.\n"
	got := roundTrip(t, input, 35)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestListItemWraps(t *testing.T) {
	input := "- The quick brown fox jumps over the lazy dog and runs away.\n"
	want := "- The quick brown fox jumps over\n  the lazy dog and runs away.\n"
	got := roundTrip(t, input, 35)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHeadingThenParagraph(t *testing.T) {
	input := "# Title\n\nSome text.\n"
	want := "# Title\n\nSome text.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCodeBlockNotWrapped(t *testing.T) {
	// Code blocks should never be wrapped regardless of width.
	input := "```\nthis is a very long line that should not be wrapped even if width is small\n```\n"
	want := "```\nthis is a very long line that should not be wrapped even if width is small\n```\n"
	got := roundTrip(t, input, 20)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMultipleInlineMarkup(t *testing.T) {
	input := "This has *italic* and **bold** and `code` in it.\n"
	want := "This has *italic* and **bold** and `code` in it.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPunctuationAfterLink(t *testing.T) {
	input := "Visit [example](https://example.com).\n"
	want := "Visit [example](https://example.com).\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPunctuationAfterEmphasis(t *testing.T) {
	input := "This is *important*, really.\n"
	want := "This is *important*, really.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStrikethrough(t *testing.T) {
	input := "This is ~~deleted~~ text.\n"
	want := "This is ~~deleted~~ text.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTaskList(t *testing.T) {
	input := "- [x] Done\n- [ ] Not done\n"
	want := "- [x] Done\n- [ ] Not done\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSimpleTable(t *testing.T) {
	input := "| A | B |\n| --- | --- |\n| 1 | 2 |\n"
	want := "| A   | B   |\n| --- | --- |\n| 1   | 2   |\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableAlignment(t *testing.T) {
	input := "| Left | Center | Right |\n| :--- | :---: | ---: |\n| a | b | c |\n"
	want := "| Left | Center | Right |\n| :--- | :----: | ----: |\n| a    |   b    |     c |\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFootnoteReference(t *testing.T) {
	input := "Both web and print versions of this book are both produced from the same files[^1].\n\n[^1]: [https://github.com/ts4z/barge-rulebook/](https://github.com/ts4z/barge-rulebook/)\n"
	want := "Both web and print versions of this book are both produced from the same\nfiles[^1].\n\n[^1]: [https://github.com/ts4z/barge-rulebook/](https://github.com/ts4z/barge-rulebook/)\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDoubleSpaceAfterSentence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "period",
			input: "First sentence. Second sentence.\n",
			want:  "First sentence.  Second sentence.\n",
		},
		{
			name:  "question mark",
			input: "Is it done? Yes it is.\n",
			want:  "Is it done?  Yes it is.\n",
		},
		{
			name:  "exclamation",
			input: "Wow! That is great.\n",
			want:  "Wow!  That is great.\n",
		},
		{
			name:  "abbreviation mid-word not affected",
			input: "Use the fmt package.\n",
			want:  "Use the fmt package.\n",
		},
		{
			name:  "comma not doubled",
			input: "Hello, world.\n",
			want:  "Hello, world.\n",
		},
		{
			name:  "closing quote after period",
			input: "He said \"hello.\" Then he left.\n",
			want:  "He said \"hello.\"  Then he left.\n",
		},
		{
			name:  "i.e. is not sentence end",
			input: "A low hand (i.e. just like razz).\n",
			want:  "A low hand (i.e. just like razz).\n",
		},
		{
			name:  "e.g. is not sentence end",
			input: "Use a tool (e.g. mdindent) for this.\n",
			want:  "Use a tool (e.g. mdindent) for this.\n",
		},
		{
			name:  "period before lowercase not doubled",
			input: "The value of x. is used here.\n",
			want:  "The value of x. is used here.\n",
		},
	}
	opts := mdio.RenderOptions{Width: 79, TwoSpacesAfterSentence: true}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTripOpts(t, tc.input, opts)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultSingleSpaceAfterSentence(t *testing.T) {
	input := "First sentence. Second sentence.\n"
	want := "First sentence. Second sentence.\n"
	got := roundTrip(t, input, 79)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFootnotePositionPreserved(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "footnote between paragraphs",
			input: "First paragraph with a reference[^1].\n\n[^1]: The footnote text.\n\nSecond paragraph.\n",
			want:  "First paragraph with a reference[^1].\n\n[^1]: The footnote text.\n\nSecond paragraph.\n",
		},
		{
			name:  "footnote at end stays at end",
			input: "First paragraph.\n\nSecond paragraph with ref[^1].\n\n[^1]: Footnote at the end.\n",
			want:  "First paragraph.\n\nSecond paragraph with ref[^1].\n\n[^1]: Footnote at the end.\n",
		},
		{
			name:  "multiple footnotes preserve order",
			input: "Para one[^1].\n\n[^1]: First note.\n\nPara two[^2].\n\n[^2]: Second note.\n",
			want:  "Para one[^1].\n\n[^1]: First note.\n\nPara two[^2].\n\n[^2]: Second note.\n",
		},
		{
			name:  "duplicate reference not re-rendered",
			input: "Para one[^1].\n\n[^1]: The note.\n\nPara two also uses[^1].\n",
			want:  "Para one[^1].\n\n[^1]: The note.\n\nPara two also uses[^1].\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.input, 79)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestLinkTextBreaksAcrossLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{
			name:  "break inside link text",
			input: "The best five-card [Action Razz](action-razz.md) hand and the best four-card [Badugi](badugi.md) hand split the pot.\n",
			width: 72,
			want:  "The best five-card [Action Razz](action-razz.md) hand and the best\nfour-card [Badugi](badugi.md) hand split the pot.\n",
		},
		{
			name:  "break between link bracket and text",
			input: "There is no qualifier for either the [Action Razz](action-razz.md) or [Badugi](badugi.md) hand.\n",
			width: 50,
			want:  "There is no qualifier for either the [Action\nRazz](action-razz.md) or [Badugi](badugi.md) hand.\n",
		},
		{
			name:  "image text breaks",
			input: "See the ![important diagram label](diagram.png) for details.\n",
			width: 40,
			want:  "See the ![important diagram\nlabel](diagram.png) for details.\n",
		},
		{
			name:  "single-word link stays atomic",
			input: "Visit [example](https://example.com) for details.\n",
			width: 79,
			want:  "Visit [example](https://example.com) for details.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.input, tc.width)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
