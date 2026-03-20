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
	if compactModeEnabled() {
		return 16
	}
	return 24
}

func inputSideIndent() float32 {
	if compactModeEnabled() {
		return 12
	}
	return 18
}


func floatingCardOuterHPad() float32 {
	if compactModeEnabled() {
		return 6
	}
	return 10
}

func floatingCardBPad() float32 {
	if compactModeEnabled() {
		return 6
	}
	return 10
}

// floatingCardBottomPad returns the scroll-bottom spacer height so the last
// message is visible above the floating input card overlay.
func floatingCardBottomPad() float32 {
	// single-line entry height ≈ textSize + padding; card + bPad ≈ 50dp
	if compactModeEnabled() {
		return 42
	}
	return 50
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
		return color.NRGBA{R: 243, G: 243, B: 246, A: 255}
	}
	return color.Transparent
}

func hoverSenderTextSize() float32 {
	size := float32(theme.TextSize())
	if compactModeEnabled() {
		size -= 2
	} else {
		size -= 1
	}
	if size < 8 {
		size = 8
	}
	return size
}

func hoverTimestampTextSize() float32 {
	size := hoverSenderTextSize() - 1
	if size < 8 {
		size = 8
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
	if size < 8 {
		size = 8
	}
	return size
}
