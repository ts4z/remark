# remark

`remark` is a Markdown prettyprinter/indent utility.  It uses the Goldmark
library and is written in Go.  The philosophy is somewhat similar to
[Go's](https://go.dev/) `[gofmt](https://pkg.go.dev/cmd/gofmt)`.

By default, it puts two spaces after a sentence, as typewriters have always
done.  The definition of sentence is somewhat heuristic and can probably be
improved.

## LLM-output code

The code here is the product of an argument between a developer and GitHub
Copilot and some Anthropic model.  It is not particularly pretty, but it is
reasonably fast and passes tests.

## style

This is a matter of opinion.  This utility has one, and mostly only one.

The style here is consistent and reasonable, but somewhat accidental.  Simple
text should be consistently line-wrapped.  Lists will be indented consistently
and numbered consistently.  Tables will be rendered neatly.

### Emacs compatibility

The maintainer is a longtime Emacs addict, so this is mostly compatible with
Emacs' `fill-paragraph` and Emacs' `markdown-mode`.  Our treatment of tables
is slightly different, because we will center-justify text within a table column
based on the header.  I think Emacs should do this, too, so I haven't changed
the behavior.

### line length

Line length is configurable with the `-w` switch.

### two spaces at the end of a sentence

Since Markdown is usually presented in a fixed-width font, I prefer two spaces
at the end of a sentence.  (If you render the text as HTML or LaTeX, render it
however you think looks best.)  But I have discovered sometimes, there is only
a single space at the end of a sentence. `remark` tries to have it both ways.
It tries to preserve what is there, but in situations where a line ends in
punctuation, the next re-wrap will have some other answer.  C'est la vie.  The
implemented behavior tries to respect the text, but with only a modern amount
of hackery.  Use the `-1` flag if you want one space when `remark` has to
decide.
