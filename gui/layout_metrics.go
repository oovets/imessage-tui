package gui

import (
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

func inputBottomGapHeight() float32 {
	if compactModeEnabled() {
		return 8
	}
	return 12
}

func hiddenInputSpacerHeight() float32 {
	if compactModeEnabled() {
		return 8
	}
	return 12
}

func inputRevealSlideHeight() float32 {
	if compactModeEnabled() {
		return 10
	}
	return 14
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
