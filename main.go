package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/ts4z/mdindent/mdparser"
)

var (
	width                 int
	oneSpaceAfterSentence bool
)

func initFlags() {
	pflag.IntVarP(&width, "width", "w", 79, "line width for output")
	pflag.BoolVarP(&oneSpaceAfterSentence, "one-space-after-sentence", "1", false, "one space after sentence-ending punctuation (default is two)")
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
// atomically (via temp file + rename).
func processFile(p *mdparser.Parser, filename string) error {
	source, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filename, err)
	}

	// Stat before processing so we can preserve permissions.
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filename, err)
	}

	// Create temp file in the same directory so rename is atomic
	// (same filesystem).
	dir := filepath.Dir(filename)
	out, err := os.CreateTemp(dir, ".mdindent-*")
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

	if err := process(p, source, out); err != nil {
		out.Close()
		return fmt.Errorf("processing %s: %w", filename, err)
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

	p := mdparser.NewParser(
		mdparser.WithWidth(width),
		mdparser.WithOneSpaceAfterSentence(oneSpaceAfterSentence),
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
			fmt.Fprintf(os.Stderr, "mdindent: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "mdindent: %v\n", err)
		}
		os.Exit(1)
	}
}
