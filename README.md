# remark

`remark` is a Markdown prettyprinter/indent utility.  It RE-indents MARKdown.
It uses the Goldmark library and is written in Go.  The philosophy is somewhat
similar to [Go's](https://go.dev/) `[gofmt](https://pkg.go.dev/cmd/gofmt)`.

## LLM-output code

The code here is the product of an argument between a developer and GitHub
Copilot and Claude Opus 4.6.  It is not particularly pretty, but it is
reasonably fast and passes tests.

## style

This is a matter of opinion.  This utility has one, and mostly only one.

### Leave it alone

By default, the philosophy of `remark` is to pass through the input with as
little re-indenting as possible.  Markdown doesn't let us embed comments that
might otherwise control the behavior, so a conservative approach is justified.
Fortunately, Goldmark does a pretty good job parsing such that we can
reconstruct the input.

This is intended for easy `diff(1)`-ability.  You can run `remark` on a
directory full of files, and it will try and not change anything.

### Hugo support

This program was written primarily to work with a mostly-Markdown Hugo site.
As such, we support both Hugo frontmatter as well as Hugo shortcodes.

Frontmatter is canonicalized to YAML and may be reorganized.  For this we use
the github.com/adrg/frontmatter package.

Hugo shortcodes are indented explicitly in a canonical form depending on length
and complexity.  Short ones will be rendered inline; longer ones will be broken
up over mutiple lines with a format that will likely make Lisp users happy.

### Unicode

Input data is assumed to be in UTF-8.  Remark understands enough about glyph
width in order to render double-wide characters reasonably (using the
github.com/mattn/go-runewith package).

### Emacs compatibility

The maintainer is a longtime Emacs addict, so this is mostly consistent with
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

If you prefer one space in these situations, use the `-1` switch.  This is
currently the only place where the behavior is configurable.

## Format stability

Format stability is NOT guaranteed, as this is still evolving.

## Idempotency

Running `remark` on a file twice should result in the same output after the
first and second run.

## TODO

This currently supports only the extensions that I use, but I see no reason not
to support more.
