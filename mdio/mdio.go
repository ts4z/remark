package mdio

import "io"

type Renderable interface {
	Render(w io.Writer, width int) error
}
