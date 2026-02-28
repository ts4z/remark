// Package passthrough provides a stub Parser/Renderable that passes
// Markdown source through unchanged.  It exists so the harness can be
// tested without a real Markdown parser.
package passthrough

import (
	"io"

	"github.com/ts4z/mdindent/mdio"
)

// renderable holds raw source and writes it back unchanged.
type renderable struct {
	source []byte
}

// Render writes the raw source to w, ignoring options.
func (r *renderable) Render(w io.Writer, opts mdio.RenderOptions) error {
	_, err := w.Write(r.source)
	return err
}

// Parser is a stub parser that returns source unchanged.
type Parser struct{}

// Parse returns a Renderable that will write source back unchanged.
func (p *Parser) Parse(source []byte) (mdio.Renderable, error) {
	return &renderable{source: source}, nil
}
