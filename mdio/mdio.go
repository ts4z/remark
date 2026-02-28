package mdio

import "io"

type RenderOption func(*RenderOptions)

func WithWidth(width int) RenderOption {
	return func(opts *RenderOptions) {
		opts.Width = width
	}
}

func WithTwoSpacesAfterSentence(twoSpaces bool) RenderOption {
	return func(opts *RenderOptions) {
		opts.TwoSpacesAfterSentence = twoSpaces
	}
}

// RenderOptions holds formatting options for rendering.
type RenderOptions struct {
	Width                  int  // line width for wrapping
	TwoSpacesAfterSentence bool // use two spaces after sentence-ending punctuation
}

type Renderable interface {
	Render(w io.Writer, opts RenderOptions) error
}
