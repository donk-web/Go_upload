package ui

import (
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type fixedTheme struct {
	background color.Color
}

func NewFixedTheme(backgroundHex string) fyne.Theme {
	return &fixedTheme{
		background: parseHexColor(backgroundHex, color.NRGBA{R: 0xf7, G: 0xf9, B: 0xfc, A: 0xff}),
	}
}

func (t *fixedTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground, theme.ColorNameMenuBackground:
		return t.background
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantLight)
	}
}

func (t *fixedTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *fixedTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *fixedTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}

func parseHexColor(value string, fallback color.NRGBA) color.Color {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return fallback
	}

	r, errR := strconv.ParseUint(value[0:2], 16, 8)
	g, errG := strconv.ParseUint(value[2:4], 16, 8)
	b, errB := strconv.ParseUint(value[4:6], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return fallback
	}

	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
}
