// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "github.com/framegrace/texelation/apps/texelterm/parser"

func init() {
	parser.MainScreenFactory = func(width, height int) parser.MainScreen {
		return NewTerminal(width, height)
	}
}
