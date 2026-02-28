package mdio

import "io"

// Renderable is a parsed document that can be rendered to a writer.
type Renderable interface {
	Render(w io.Writer) error
}
