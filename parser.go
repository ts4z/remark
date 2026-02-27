package main

import "github.com/ts4z/mdindent/mdio"

// Parser parses Markdown source into a Renderable.
type Parser interface {
	Parse(source []byte) (mdio.Renderable, error)
}
