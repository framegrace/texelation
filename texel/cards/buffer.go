package cards

import texelcore "github.com/framegrace/texelui/core"

func cloneBuffer(input [][]texelcore.Cell) [][]texelcore.Cell {
	out := make([][]texelcore.Cell, len(input))
	for i := range input {
		out[i] = make([]texelcore.Cell, len(input[i]))
		copy(out[i], input[i])
	}
	return out
}
