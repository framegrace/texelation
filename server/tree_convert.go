package server

import (
	"encoding/json"

	"texelation/protocol"
	"texelation/texel"
)

func treeCaptureToProtocol(capture texel.TreeCapture) protocol.TreeSnapshot {
	snapshot := protocol.TreeSnapshot{Panes: make([]protocol.PaneSnapshot, len(capture.Panes))}
	for i, pane := range capture.Panes {
		rows := make([]string, len(pane.Buffer))
		for y, row := range pane.Buffer {
			runes := make([]rune, len(row))
			for x, cell := range row {
				if cell.Ch == 0 {
					runes[x] = ' '
				} else {
					runes[x] = cell.Ch
				}
			}
			rows[y] = string(runes)
		}
		snapshot.Panes[i] = protocol.PaneSnapshot{
			PaneID:    pane.ID,
			Revision:  0,
			Title:     pane.Title,
			Rows:      rows,
			X:         int32(pane.Rect.X),
			Y:         int32(pane.Rect.Y),
			Width:     int32(pane.Rect.Width),
			Height:    int32(pane.Rect.Height),
			AppType:   pane.AppType,
			AppConfig: encodeAppConfig(pane.AppConfig),
		}
	}
	snapshot.Root = buildProtocolTreeNode(capture.Root)
	return snapshot
}

func protocolToTreeCapture(snapshot protocol.TreeSnapshot) texel.TreeCapture {
	capture := texel.TreeCapture{Panes: make([]texel.PaneSnapshot, len(snapshot.Panes))}
	for i, pane := range snapshot.Panes {
		buffer := make([][]texel.Cell, len(pane.Rows))
		for y, row := range pane.Rows {
			runes := []rune(row)
			buffer[y] = make([]texel.Cell, len(runes))
			for x, r := range runes {
				buffer[y][x] = texel.Cell{Ch: r}
			}
		}
		capture.Panes[i] = texel.PaneSnapshot{
			ID:        pane.PaneID,
			Title:     pane.Title,
			Buffer:    buffer,
			Rect:      texel.Rectangle{X: int(pane.X), Y: int(pane.Y), Width: int(pane.Width), Height: int(pane.Height)},
			AppType:   pane.AppType,
			AppConfig: decodeAppConfig(pane.AppConfig),
		}
	}
	capture.Root = protocolNodeToCapture(snapshot.Root)
	return capture
}

func protocolNodeToCapture(node protocol.TreeNodeSnapshot) *texel.TreeNodeCapture {
	capture := &texel.TreeNodeCapture{PaneIndex: int(node.PaneIndex)}
	if len(node.Children) == 0 {
		return capture
	}
	switch node.Split {
	case protocol.SplitVertical:
		capture.Split = texel.Vertical
	case protocol.SplitHorizontal:
		capture.Split = texel.Horizontal
	default:
		capture.Split = texel.Vertical
	}
	capture.SplitRatios = make([]float64, len(node.SplitRatios))
	for i, ratio := range node.SplitRatios {
		capture.SplitRatios[i] = float64(ratio)
	}
	if len(capture.SplitRatios) != len(node.Children) {
		capture.SplitRatios = make([]float64, len(node.Children))
		if len(node.Children) > 0 {
			equal := 1.0 / float64(len(node.Children))
			for i := range capture.SplitRatios {
				capture.SplitRatios[i] = equal
			}
		}
	}
	capture.Children = make([]*texel.TreeNodeCapture, len(node.Children))
	for i := range node.Children {
		copyChild := node.Children[i]
		capture.Children[i] = protocolNodeToCapture(copyChild)
	}
	return capture
}

func cloneProtocolTree(node protocol.TreeNodeSnapshot) protocol.TreeNodeSnapshot {
	clone := protocol.TreeNodeSnapshot{
		PaneIndex:   node.PaneIndex,
		Split:       node.Split,
		SplitRatios: make([]float32, len(node.SplitRatios)),
		Children:    make([]protocol.TreeNodeSnapshot, len(node.Children)),
	}
	copy(clone.SplitRatios, node.SplitRatios)
	for i, child := range node.Children {
		clone.Children[i] = cloneProtocolTree(child)
	}
	return clone
}

func encodeAppConfig(cfg map[string]interface{}) string {
	if cfg == nil {
		return ""
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeAppConfig(data string) map[string]interface{} {
	if data == "" {
		return nil
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return nil
	}
	return cfg
}

func buildProtocolTreeNode(node *texel.TreeNodeCapture) protocol.TreeNodeSnapshot {
	if node == nil {
		return protocol.TreeNodeSnapshot{PaneIndex: -1, Split: protocol.SplitNone}
	}
	protoNode := protocol.TreeNodeSnapshot{PaneIndex: int32(node.PaneIndex)}
	if len(node.Children) == 0 {
		protoNode.Split = protocol.SplitNone
		return protoNode
	}
	switch node.Split {
	case texel.Vertical:
		protoNode.Split = protocol.SplitVertical
	case texel.Horizontal:
		protoNode.Split = protocol.SplitHorizontal
	default:
		protoNode.Split = protocol.SplitNone
	}
	protoNode.SplitRatios = make([]float32, len(node.SplitRatios))
	for i, ratio := range node.SplitRatios {
		protoNode.SplitRatios[i] = float32(ratio)
	}
	protoNode.Children = make([]protocol.TreeNodeSnapshot, len(node.Children))
	for i, child := range node.Children {
		protoNode.Children[i] = buildProtocolTreeNode(child)
	}
	return protoNode
}
