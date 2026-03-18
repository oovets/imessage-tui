package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// glyphAction is a flat clickable text glyph (no button chrome/background).
type glyphAction struct {
	widget.Label
	onTap      func()
	emphasized bool
}

func newGlyphAction(text string, onTap func()) *glyphAction {
	g := &glyphAction{onTap: onTap}
	g.SetText(text)
	g.Alignment = fyne.TextAlignCenter
	g.TextStyle = fyne.TextStyle{}
	g.SetEmphasis(false)
	g.ExtendBaseWidget(g)
	return g
}

func (g *glyphAction) SetEmphasis(on bool) {
	g.emphasized = on
	if g.emphasized {
		// Match normal message text color when active/hovered.
		g.Importance = widget.MediumImportance
	} else {
		g.Importance = widget.LowImportance
	}
	g.Refresh()
}

func (g *glyphAction) Tapped(_ *fyne.PointEvent) {
	if g.onTap != nil {
		g.onTap()
	}
}

func (g *glyphAction) TappedSecondary(_ *fyne.PointEvent) {}
