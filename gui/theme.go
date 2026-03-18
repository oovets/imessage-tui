package gui

import (
	"image/color"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type fontSet struct {
	regular, bold, italic, boldItalic fyne.Resource
}

type fontDef struct {
	Name                              string
	Regular, Bold, Italic, BoldItalic string
}

var knownFonts = []fontDef{
	{Name: "Default"},
	{
		Name:       "Lato",
		Regular:    "/usr/share/fonts/TTF/Lato-Regular.ttf",
		Bold:       "/usr/share/fonts/TTF/Lato-Bold.ttf",
		Italic:     "/usr/share/fonts/TTF/Lato-Italic.ttf",
		BoldItalic: "/usr/share/fonts/TTF/Lato-BoldItalic.ttf",
	},
	{
		Name:       "Inter",
		Regular:    "/usr/share/fonts/inter/Inter-Regular.otf",
		Bold:       "/usr/share/fonts/inter/Inter-Bold.otf",
		Italic:     "/usr/share/fonts/inter/Inter-Italic.otf",
		BoldItalic: "/usr/share/fonts/inter/Inter-BoldItalic.otf",
	},
	{
		Name:       "Noto Sans",
		Regular:    "/usr/share/fonts/noto/NotoSans-Regular.ttf",
		Bold:       "/usr/share/fonts/noto/NotoSans-Bold.ttf",
		Italic:     "/usr/share/fonts/noto/NotoSans-Italic.ttf",
		BoldItalic: "/usr/share/fonts/noto/NotoSans-BoldItalic.ttf",
	},
}

func loadFont(path string) fyne.Resource {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return fyne.NewStaticResource(path, data)
}

// compactTheme is a mutable theme supporting dark/light mode, font size, font family, and bold.
type compactTheme struct {
	dark      bool
	fontSize  float32
	boldAll   bool
	fonts     map[string]fontSet
	curFamily string
}

func newCompactTheme() *compactTheme {
	t := &compactTheme{
		dark:     true,
		fontSize: 11,
		fonts:    make(map[string]fontSet),
	}
	// Load all installed font families.
	for _, def := range knownFonts {
		if def.Name == "Default" {
			t.fonts["Default"] = fontSet{} // all nil → Fyne built-in
			continue
		}
		regular := loadFont(def.Regular)
		if regular == nil {
			continue // not installed
		}
		t.fonts[def.Name] = fontSet{
			regular:    regular,
			bold:       loadFont(def.Bold),
			italic:     loadFont(def.Italic),
			boldItalic: loadFont(def.BoldItalic),
		}
	}
	// Prefer Lato if installed.
	if _, ok := t.fonts["Lato"]; ok {
		t.curFamily = "Lato"
	} else {
		t.curFamily = "Default"
	}
	return t
}

// availableFamilies returns the names of installed font families in display order.
func (t *compactTheme) availableFamilies() []string {
	out := []string{"Default"}
	for _, def := range knownFonts {
		if def.Name == "Default" {
			continue
		}
		if _, ok := t.fonts[def.Name]; ok {
			out = append(out, def.Name)
		}
	}
	return out
}

func (t *compactTheme) base() fyne.Theme {
	if t.dark {
		return theme.DarkTheme()
	}
	return theme.LightTheme()
}

func (t *compactTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if !t.dark && name == theme.ColorNameSuccess {
		return color.NRGBA{R: 140, G: 140, B: 140, A: 255}
	}
	return t.base().Color(name, variant)
}

func (t *compactTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base().Icon(name)
}

func (t *compactTheme) Font(style fyne.TextStyle) fyne.Resource {
	if t.boldAll {
		style.Bold = true
	}
	fs := t.fonts[t.curFamily]
	switch {
	case style.Bold && style.Italic:
		if fs.boldItalic != nil {
			return fs.boldItalic
		}
	case style.Bold:
		if fs.bold != nil {
			return fs.bold
		}
	case style.Italic:
		if fs.italic != nil {
			return fs.italic
		}
	default:
		if fs.regular != nil {
			return fs.regular
		}
	}
	return t.base().Font(style)
}

func (t *compactTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 2
	case theme.SizeNameInnerPadding:
		return 4
	case theme.SizeNameScrollBar, theme.SizeNameScrollBarSmall:
		return 0
	case theme.SizeNameText, theme.SizeNameSubHeadingText:
		return t.fontSize
	default:
		return t.base().Size(name)
	}
}
