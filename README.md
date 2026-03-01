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

### Leave it alone

By default, our philosophy is to pass through the input with as little
re-indenting as possible.  Markdown isn't very flexible here, in that it
doesn't allow comments or other metadata.

This is intended for easy `diff(1)`-ability.  You can run `remark` on a
directory full of files, and it will try and not change anything.

### Emacs compatibility

The maintainer is a longtime Emacs addict, so this is mostly compatible with
Emacs' `fill-paragraph` and Emacs' `markdown-mode`.  Our treatment of tables is
slightly different, because we will center-justify text within a table column
based on the header.  I think Emacs should do this, too, so I haven't changed
the behavior.

### line length

Line length is configurable with the `-w` switch.

### two spaces at the end of a sentence (maybe)

According to the [Leave it alone](#leave-it-alone) rule, we try not to change
spacing at the end of a sentence.  Whatever you have, `remark` will attempt to
preserve.  Sometimes it is unclear, however, such as at the end of a line.
Since Markdown "source" is almost always rendered in fixed-width text, and the
maintainer learned to type in the '80s, the default is to put two spaces at the
end of a sentence (if we can correctly automatically identify the end of a
sentence).

If you prefer one space in these situations, use the `-1` switch.
