package mdparser

import "github.com/mattn/go-runewidth"

// displayWidth returns the number of terminal columns that s occupies,
// assuming a fixed-width font.
//
// This is not the same as utf8.RuneCountInString: multi-rune grapheme
// clusters can occupy a single column (combining diacritics, variation
// selectors, zero-width joiners) or two columns (CJK ideographs, many
// emoji).  Regional-indicator pairs that form a flag emoji, and ZWJ
// sequences that compose a single glyph, are each counted as one unit.
func displayWidth(s string) int {
	return runewidth.StringWidth(s)
}
