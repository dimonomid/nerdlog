package ui

import (
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// stringWidth returns the number of horizontal cells needed to print the given
// text. It splits the text into its grapheme clusters, calculates each
// cluster's width, and adds them up to a total.
func stringWidth(text string) (width int) {
	// Calculate the display width of the input string by processing grapheme clusters.
	// This function iterates through each grapheme cluster in the string and sums up
	// the widths of the runes, focusing on the first non-zero-width rune for accuracy.
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		var chWidth int
		for _, r := range g.Runes() {
			chWidth = runewidth.RuneWidth(r)
			if chWidth > 0 {
				break // Use the width of the first non-zero-width rune in the cluster
			}
		}
		width += chWidth
	}
	return width // Return the total calculated width
}
