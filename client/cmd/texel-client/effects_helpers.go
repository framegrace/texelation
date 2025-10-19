package main

import (
	"math"

	"github.com/gdamore/tcell/v2"
)

var (
	defaultInactiveColor = tcell.NewRGBColor(20, 20, 32)
	defaultFlashColor    = tcell.NewRGBColor(255, 255, 255)
	defaultResizingColor = tcell.NewRGBColor(255, 184, 108)
)

func tintStyle(style tcell.Style, overlay tcell.Color, intensity float32) tcell.Style {
	if intensity <= 0 {
		return style
	}
	fg, bg, attrs := style.Decompose()
	if !fg.Valid() {
		fg = tcell.ColorWhite
	}
	if !bg.Valid() {
		bg = tcell.ColorBlack
	}
	blendedFg := blendColor(fg, overlay, intensity)
	blendedBg := blendColor(bg, overlay, intensity)
	return tcell.StyleDefault.Foreground(blendedFg).
		Background(blendedBg).
		Bold(attrs&tcell.AttrBold != 0).
		Underline(attrs&tcell.AttrUnderline != 0).
		Reverse(attrs&tcell.AttrReverse != 0).
		Blink(attrs&tcell.AttrBlink != 0).
		Dim(attrs&tcell.AttrDim != 0).
		Italic(attrs&tcell.AttrItalic != 0)
}

func blendColor(base, overlay tcell.Color, intensity float32) tcell.Color {
	if !overlay.Valid() || intensity <= 0 {
		return base
	}
	if !base.Valid() {
		return overlay
	}
	br, bg, bb := base.RGB()
	or, og, ob := overlay.RGB()
	blend := func(bc, oc int32) int32 {
		return int32(float32(bc)*(1-intensity) + float32(oc)*intensity)
	}
	return tcell.NewRGBColor(blend(br, or), blend(bg, og), blend(bb, ob))
}

func hsvToRGB(angle float32, saturation float32, value float32) tcell.Color {
	h := float32(math.Mod(float64(angle), 2*math.Pi)) / (2 * math.Pi) * 360
	c := value * saturation
	x := c * (1 - float32(math.Abs(math.Mod(float64(h/60), 2)-1)))
	m := value - c
	var r, g, b float32
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	r, g, b = (r+m)*255, (g+m)*255, (b+m)*255
	return tcell.NewRGBColor(int32(r), int32(g), int32(b))
}
