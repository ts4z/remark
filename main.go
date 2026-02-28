package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/ts4z/mdindent/mdio"
	"github.com/ts4z/mdindent/mdparser"
)

var (
	width                 int
	twoSpacesAft
	erSentence bool
)

func initFlags() {
	pflag.IntVarP(&width, "width", "w", 79, "line width for output")
	pflag.BoolVarP(&twoSpacesAfterSentence, "two-spaces", "2", false, "two spaces after sentence-ending punctuation")
}

// process parses source with the given Parser and renders the result to w.
func process(p Parser, source []byte, w io.Writer, opts mdio.RenderOptions) error {
	r, err := p.Parse(source)
	if err != nil {
		return err
	}
	return r.Render(w, opts)
}

// processFile reads a file, processes it, and writes the result back
// atomically (via temp file + rename).
func processFile(p Parser, filename string) error {
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
	tmp, err := os.CreateTemp(dir, ".mdindent-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(info.Mode()); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}

	opts := mdio.RenderOptions{Width: width, TwoSpacesAfterSentence: twoSpacesAferSentence}
	if err := process(p, source, tmp, opts); err != nil {
		tmp.Close()
		return fmt.Errorf("processing %s: %w", filename, err)
	}

	if err := tmp.Close(); err != nil {
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

	p := &mdparser.Parser{}

	args := pflag.Args()
	if len(args) == 0 {
		source, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		opts := mdio.RenderOptions{Width: width, TwoSpacesAfterSentence: twoSpacesAferSentence}
		return process(p, source, os.Stdout, opts)
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
