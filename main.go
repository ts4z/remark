package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/ts4z/remark/mdparser"
)

const (
	defaultWidth = 79
)

var (
	width                 int
	oneSpaceAfterSentence bool
	noFrontmatter         bool
	quiet                 bool
)

func initFlags() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, `remark: *re*indent *mark*down

Usage: %s [OPTIONS] [FILES...]

If no files are supplied, remark will operate on stdin/stdout.

`,
			filepath.Base(os.Args[0]))
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Note on inter-sentence spacing: Space is generally preserved.  The -1 switch
controls only what happens when it isn't.  Consider re-wrapping a paragraph
where one line ends in a period.  In this case, remark has to insert space
between the end of this sentence and the next sentence.  It can either add two
spaces, as typists were taught to for many years; or it can add one, as modern
style manuals suggest.  End-of-sentence detection is also heuristic; that is,
it is sometimes wrong.

By default, remark tries to detect (and preserve) frontmatter.  If the
--no-frontmatter switch is given, the whole file will be blindly parsed as
Markdown.

remark supports all "standard" Markdown, plus GFM tables, footnotes, and
definition lists.
`)
	}
	pflag.IntVarP(&width, "width", "w", defaultWidth, "line width for output")
	pflag.BoolVarP(&oneSpaceAfterSentence, "one-space-after-sentence", "1", false, "put one space between sentences (default two)*")
	pflag.BoolVar(&noFrontmatter, "no-frontmatter", false, "don't look for frontmatter")
	pflag.BoolVarP(&quiet, "quiet", "q", false, "less loquacious, more laconic")
}

// process parses source with the given Parser and renders the result to w.
func process(p *mdparser.Parser, source []byte, w io.Writer) error {
	r, err := p.Parse(source)
	if err != nil {
		return err
	}
	return r.Render(w)
}

// processFile reads a file, processes it, and writes the result back
// atomically (via temp file + rename).  If the output is identical to
// the input, the file is left untouched.
func processFile(p *mdparser.Parser, filename string) error {
	source, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filename, err)
	}

	// Render into memory so we can compare before writing.
	var buf bytes.Buffer
	if err := process(p, source, &buf); err != nil {
		return fmt.Errorf("processing %s: %w", filename, err)
	}

	// If nothing changed, skip the write entirely.
	if bytes.Equal(source, buf.Bytes()) {
		return nil
	}

	// Stat before writing so we can preserve permissions.
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filename, err)
	}

	// Create temp file in the same directory so rename is atomic
	// (same filesystem).
	dir := filepath.Dir(filename)
	out, err := os.CreateTemp(dir, "tmp.remark-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := out.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if err := out.Chmod(info.Mode()); err != nil {
		out.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if _, err := out.Write(buf.Bytes()); err != nil {
		out.Close()
		return fmt.Errorf("writing %s: %w", filename, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	tmpName = "" // prevent deferred cleanup
	return nil
}

func run() error {
	pflag.Parse()

	var warn mdparser.WarnFunc
	if !quiet {
		warn = func(format string, args ...interface{}) {
			fmt.Fprintf(os.Stderr, "remark: warning: "+format+"\n", args...)
		}
	}

	p := mdparser.NewParser(
		mdparser.WithWidth(width),
		mdparser.WithOneSpaceAfterSentence(oneSpaceAfterSentence),
		mdparser.WithFrontmatter(!noFrontmatter),
		mdparser.WithWarnFunc(warn),
	)

	args := pflag.Args()
	if len(args) == 0 {
		source, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		return process(p, source, os.Stdout)
	}

	var firstErr error
	for _, filename := range args {
		if err := processFile(p, filename); err != nil {
			fmt.Fprintf(os.Stderr, "remark: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func main() {
	initFlags()

	if err := run(); err != nil {
		// For stdin mode, the error hasn't been printed yet.
		if pflag.NArg() == 0 {
			fmt.Fprintf(os.Stderr, "remark: %v\n", err)
		}
		os.Exit(1)
	}
}
