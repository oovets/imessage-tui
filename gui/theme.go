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
	Regular, Bold, Italic, BoldItalic []string
}

var knownFonts = []fontDef{
	{Name: "Default"},
	{
		Name:       "Lato",
		Regular:    []string{"/usr/share/fonts/TTF/Lato-Regular.ttf"},
		Bold:       []string{"/usr/share/fonts/TTF/Lato-Bold.ttf"},
		Italic:     []string{"/usr/share/fonts/TTF/Lato-Italic.ttf"},
		BoldItalic: []string{"/usr/share/fonts/TTF/Lato-BoldItalic.ttf"},
	},
	{
		Name:       "Inter",
		Regular:    []string{"/usr/share/fonts/inter/Inter-Regular.otf"},
		Bold:       []string{"/usr/share/fonts/inter/Inter-Bold.otf"},
		Italic:     []string{"/usr/share/fonts/inter/Inter-Italic.otf"},
		BoldItalic: []string{"/usr/share/fonts/inter/Inter-BoldItalic.otf"},
	},
	{
		Name:       "Noto Sans",
		Regular:    []string{"/usr/share/fonts/noto/NotoSans-Regular.ttf"},
		Bold:       []string{"/usr/share/fonts/noto/NotoSans-Bold.ttf"},
		Italic:     []string{"/usr/share/fonts/noto/NotoSans-Italic.ttf"},
		BoldItalic: []string{"/usr/share/fonts/noto/NotoSans-BoldItalic.ttf"},
	},
	{
		Name: "JetBrains Mono Nerd Font",
		Regular: []string{
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFont-Regular.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFontMono-Regular.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFontMono-Regular.ttf",
		},
		Bold: []string{
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Bold.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFont-Bold.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFontMono-Bold.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFontMono-Bold.ttf",
		},
		Italic: []string{
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Italic.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFont-Italic.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFontMono-Italic.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFontMono-Italic.ttf",
		},
		BoldItalic: []string{
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-BoldItalic.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFont-BoldItalic.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNerdFontMono-BoldItalic.ttf",
			"/usr/share/fonts/TTF/JetBrainsMonoNLNerdFontMono-BoldItalic.ttf",
		},
	},
	{
		Name: "Geist",
		Regular: []string{
			"/usr/share/fonts/TTF/Geist-Regular.ttf",
			"/usr/share/fonts/OTF/Geist-Regular.otf",
			"/usr/share/fonts/TTF/GeistVF.ttf",
			"/usr/share/fonts/OTF/GeistVF.otf",
		},
		Bold: []string{
			"/usr/share/fonts/TTF/Geist-Bold.ttf",
			"/usr/share/fonts/OTF/Geist-Bold.otf",
			"/usr/share/fonts/TTF/GeistVF.ttf",
			"/usr/share/fonts/OTF/GeistVF.otf",
		},
		Italic: []string{
			"/usr/share/fonts/TTF/Geist-Italic.ttf",
			"/usr/share/fonts/OTF/Geist-Italic.otf",
			"/usr/share/fonts/TTF/GeistVF.ttf",
			"/usr/share/fonts/OTF/GeistVF.otf",
		},
		BoldItalic: []string{
			"/usr/share/fonts/TTF/Geist-BoldItalic.ttf",
			"/usr/share/fonts/OTF/Geist-BoldItalic.otf",
			"/usr/share/fonts/TTF/GeistVF.ttf",
			"/usr/share/fonts/OTF/GeistVF.otf",
		},
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

func loadFirstFont(paths []string) fyne.Resource {
	for _, p := range paths {
		if r := loadFont(p); r != nil {
			return r
		}
	}
	return nil
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
		regular := loadFirstFont(def.Regular)
		if regular == nil {
			continue // not installed
		}
		t.fonts[def.Name] = fontSet{
			regular:    regular,
			bold:       loadFirstFont(def.Bold),
			italic:     loadFirstFont(def.Italic),
			boldItalic: loadFirstFont(def.BoldItalic),
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
