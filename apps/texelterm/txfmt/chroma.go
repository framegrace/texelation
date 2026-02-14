// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package txfmt

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

const (
	defaultStyleName  = "catppuccin-mocha"
	maxChromaContext  = 50 // max previous lines kept for lexer context
)

// chromaStyle resolves a style name to a Chroma style, falling back to the default.
func chromaStyle(name string) *chroma.Style {
	if name == "" {
		name = defaultStyleName
	}
	return styles.Get(name)
}

// lineRegion tracks a line's rune span within the combined text.
type lineRegion struct {
	line      *parser.LogicalLine
	textStart int   // rune offset in combined text where this line starts
	textToCell []int // maps rune index (relative to textStart) → cell index
}

// chromaColorizeLines tokenizes multiple lines together as a single block and
// applies token colors/attributes to cells in-place. Multi-line tokenization
// gives the lexer full context (e.g. package/import/func structure in Go,
// bold/heading context in markdown).
//
// Only cells with default FG are modified. Tokens whose color matches the
// style's base text color are skipped to preserve the default FG bit.
func chromaColorizeLines(lines []*parser.LogicalLine, lexerName string, style *chroma.Style) {
	if len(lines) == 0 {
		return
	}

	regions, fullText := buildLineRegions(lines)
	if fullText == "" {
		return
	}

	applyChromaTokens(regions, fullText, lexerName, style)
}

// chromaColorizeWithContext tokenizes a single line using previous lines as
// lexer context. Only the current line's cells are modified.
func chromaColorizeWithContext(line *parser.LogicalLine, context []string, lexerName string, style *chroma.Style) {
	plain, textToCell := buildPlainTextMap(line.Cells)
	if len(plain) == 0 {
		return
	}
	lineText := string(plain)

	// Build combined text: context lines + current line, separated by \n.
	var sb strings.Builder
	for _, ctx := range context {
		sb.WriteString(ctx)
		sb.WriteByte('\n')
	}
	contextRuneLen := len([]rune(sb.String()))
	sb.WriteString(lineText)
	sb.WriteByte('\n') // trailing \n for line-oriented patterns

	// Build a single region for the current line only.
	regions := []lineRegion{{
		line:       line,
		textStart:  contextRuneLen,
		textToCell: textToCell,
	}}

	applyChromaTokens(regions, sb.String(), lexerName, style)
}

// buildLineRegions extracts plain text from each line and concatenates them
// with \n separators. Returns the regions and the combined text.
func buildLineRegions(lines []*parser.LogicalLine) ([]lineRegion, string) {
	regions := make([]lineRegion, 0, len(lines))
	var sb strings.Builder
	runeOffset := 0

	for _, line := range lines {
		plain, textToCell := buildPlainTextMap(line.Cells)
		if len(plain) == 0 {
			// Empty line: still emit a \n for proper line counting.
			sb.WriteByte('\n')
			runeOffset++ // the \n
			continue
		}
		regions = append(regions, lineRegion{
			line:       line,
			textStart:  runeOffset,
			textToCell: textToCell,
		})
		sb.WriteString(string(plain))
		runeOffset += len(plain)
		sb.WriteByte('\n')
		runeOffset++ // the \n
	}
	return regions, sb.String()
}

// applyChromaTokens tokenizes fullText and applies colors to cell regions.
func applyChromaTokens(regions []lineRegion, fullText, lexerName string, style *chroma.Style) {
	lexer := getLexer(lexerName, fullText)
	lexer = chroma.Coalesce(lexer)

	tokens, err := chroma.Tokenise(lexer, nil, fullText)
	if err != nil {
		return
	}

	baseColour := style.Get(chroma.Text).Colour

	// Pre-compute region lookup: for each rune position, which region (if any)?
	// We walk tokens and regions in parallel since both are ordered.
	ri := 0 // current region index
	runePos := 0

	for _, tok := range tokens {
		if tok.Type == chroma.EOFType {
			break
		}
		tokRunes := []rune(tok.Value)
		entry := style.Get(tok.Type)

		fg, attr, hasDistinctColor := resolveTokenStyle(entry, baseColour)

		for i := range tokRunes {
			absPos := runePos + i

			// Advance past regions we've passed.
			for ri < len(regions) && absPos >= regions[ri].textStart+len(regions[ri].textToCell) {
				ri++
			}
			if ri >= len(regions) {
				break
			}

			r := &regions[ri]
			localPos := absPos - r.textStart
			if localPos < 0 || localPos >= len(r.textToCell) {
				continue // in a \n separator or before this region
			}

			ci := r.textToCell[localPos]
			cells := r.line.Cells

			if hasDistinctColor {
				if attr != 0 {
					setFGAttr(&cells[ci], fg, attr)
				} else {
					setFG(&cells[ci], fg)
				}
			} else if attr != 0 && isDefaultFG(&cells[ci]) {
				// Base text color but has attributes (e.g. bold markdown text)
				cells[ci].Attr |= attr
			}
		}

		runePos += len(tokRunes)
		// Reset ri to allow overlapping — but since tokens are sequential,
		// we can keep ri as-is for efficiency. However, after processing
		// a token that spans \n boundaries, ri might have advanced too far.
		// Walk ri back if needed.
		if ri > 0 && ri < len(regions) && runePos < regions[ri].textStart {
			ri--
		}
	}
}

// resolveTokenStyle extracts color and attributes from a style entry.
// Returns hasDistinctColor=false if the color matches the base text color.
func resolveTokenStyle(entry chroma.StyleEntry, baseColour chroma.Colour) (parser.Color, parser.Attribute, bool) {
	var attr parser.Attribute
	if entry.Bold == chroma.Yes {
		attr |= parser.AttrBold
	}
	if entry.Italic == chroma.Yes {
		attr |= parser.AttrItalic
	}
	if entry.Underline == chroma.Yes {
		attr |= parser.AttrUnderline
	}

	if !entry.Colour.IsSet() || entry.Colour == baseColour {
		return parser.Color{}, attr, false
	}

	fg := parser.Color{
		Mode: parser.ColorModeRGB,
		R:    entry.Colour.Red(),
		G:    entry.Colour.Green(),
		B:    entry.Colour.Blue(),
	}
	return fg, attr, true
}

// getLexer returns a Chroma lexer by name, or auto-detects from content.
func getLexer(name, text string) chroma.Lexer {
	if name != "" {
		if l := lexers.Get(name); l != nil {
			return l
		}
	}
	if l := lexers.Analyse(text); l != nil {
		return l
	}
	return lexers.Fallback
}
