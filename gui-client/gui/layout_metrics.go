package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func compactModeEnabled() bool {
	app := fyne.CurrentApp()
	if app == nil {
		return false
	}
	t, ok := app.Settings().Theme().(*compactTheme)
	return ok && t.compactMode
}

func messageSideIndent() float32 {
	return 8
}

func inputSideIndent() float32 {
	return 52
}

func floatingCardOuterHPad() float32 {
	return 1
}

func floatingCardBPad() float32 {
	return 1
}

// floatingCardBottomPad returns the scroll-bottom spacer height so the last
// message is visible above the floating input card overlay.
func floatingCardBottomPad() float32 {
	// Kept for compatibility with older floating layout paths.
	return 60
}

func floatingInputBgColor() color.Color {
	app := fyne.CurrentApp()
	if app == nil {
		return color.Transparent
	}
	if t, ok := app.Settings().Theme().(*compactTheme); ok {
		if t.dark {
			// slightly brighter than the main dark bg (#1a1b26)
			return color.NRGBA{R: 30, G: 32, B: 50, A: 255}
		}
		// Keep the floating input subtle but visible in light mode.
		return color.NRGBA{R: 244, G: 244, B: 247, A: 245}
	}
	return theme.Color(theme.ColorNameInputBackground)
}

func hoverSenderTextSize() float32 {
	size := float32(theme.TextSize())
	if compactModeEnabled() {
		size -= 2
	} else {
		size -= 1
	}
	if size < minUIFontSize {
		size = minUIFontSize
	}
	return size
}

func hoverTimestampTextSize() float32 {
	size := hoverSenderTextSize() - 1
	if size < minUIFontSize {
		size = minUIFontSize
	}
	return size
}

func glyphTextSize() float32 {
	size := float32(theme.TextSize())
	if compactModeEnabled() {
		size -= 3
	} else {
		size -= 1
	}
	if size < minUIFontSize {
		size = minUIFontSize
	}
	return size
}
