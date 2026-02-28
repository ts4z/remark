# mdindent

mdindent is a Markdown indent utility.  It uses the Goldmark library and is
written in Go.

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

### line length

Line length is configurable with the `-w` switch.

### two spaces at the end of a sentence

Since Markdown is usually presented in a fixed-width font, I prefer two spaces
at the end of a sentence.  (If you render the text as HTML or LaTeX, render it
however you think looks best.)  But I have discovered sometimes, there is only
a single space at the end of a sentence. `mdindent` tries to have it both ways.
It tries to preserve what is there, but in situations where a line ends in
punctuation, the next re-wrap will have some other answer.  C'est la vie.  The
implemented behavior tries to respect the text, but with only a modern amount
of hackery.  Use the `-1` flag if you want one space when `mdindent` has to
decide.
