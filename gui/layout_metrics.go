package gui

import "fyne.io/fyne/v2"

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
