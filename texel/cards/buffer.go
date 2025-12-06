package cards

import "texelation/texel"

func cloneBuffer(input [][]texel.Cell) [][]texel.Cell {
	out := make([][]texel.Cell, len(input))
	for i := range input {
		out[i] = make([]texel.Cell, len(input[i]))
		copy(out[i], input[i])
	}
	return out
}
