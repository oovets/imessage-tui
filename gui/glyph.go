package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// glyphAction is a flat clickable text glyph (no button chrome/background).
type glyphAction struct {
	widget.BaseWidget
	textObj     *canvas.Text
	onTap       func()
	emphasized  bool
	currentText string
	fixedColor  color.Color
}

func newGlyphAction(text string, onTap func()) *glyphAction {
	g := &glyphAction{
		onTap:       onTap,
		currentText: text,
		textObj:     canvas.NewText(text, theme.Color(theme.ColorNameDisabled)),
	}
	g.textObj.Alignment = fyne.TextAlignCenter
	g.textObj.TextSize = glyphTextSize()
	g.ExtendBaseWidget(g)
	return g
}

func (g *glyphAction) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.textObj)
}

func (g *glyphAction) MinSize() fyne.Size {
	s := fyne.MeasureText(g.currentText, g.textObj.TextSize, fyne.TextStyle{})
	return fyne.NewSize(s.Width+6, s.Height+2)
}

func (g *glyphAction) SetText(text string) {
	g.currentText = text
	g.textObj.Text = text
	g.Refresh()
}

func (g *glyphAction) SetTextSize(size float32) {
	if size < minUIFontSize {
		size = minUIFontSize
	}
	g.textObj.TextSize = size
	g.Refresh()
}

func (g *glyphAction) SetFixedColor(c color.Color) {
	g.fixedColor = c
	g.Refresh()
}

func (g *glyphAction) SetEmphasis(on bool) {
	g.emphasized = on
	g.Refresh()
}

func (g *glyphAction) Tapped(_ *fyne.PointEvent) {
	if g.onTap != nil {
		g.onTap()
	}
}

func (g *glyphAction) TappedSecondary(_ *fyne.PointEvent) {}

func (g *glyphAction) Refresh() {
	if g.textObj == nil {
		return
	}
	if g.fixedColor != nil {
		g.textObj.Color = g.fixedColor
	} else if g.emphasized {
		g.textObj.Color = theme.Color(theme.ColorNameForeground)
	} else {
		g.textObj.Color = theme.Color(theme.ColorNameDisabled)
	}
	canvas.Refresh(g.textObj)
	g.BaseWidget.Refresh()
}
