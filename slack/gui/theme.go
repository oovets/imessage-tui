package gui

import (
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

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

var (
	alacrittyFontDottedRE = regexp.MustCompile(`(?mi)^\s*(?:normal|bold|italic|bold_italic)\.family\s*=\s*["']?([^"'\n#]+)`)
	alacrittyFontFlatRE   = regexp.MustCompile(`(?mi)^\s*family\s*[:=]\s*["']?([^"'\n#]+)`)
)

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

func detectAlacrittyFontFamily() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	paths := []string{
		filepath.Join(home, ".config", "alacritty", "alacritty.toml"),
		filepath.Join(home, ".alacritty.toml"),
		filepath.Join(home, ".config", "alacritty", "alacritty.yml"),
		filepath.Join(home, ".config", "alacritty", "alacritty.yaml"),
		filepath.Join(home, ".alacritty.yml"),
		filepath.Join(home, ".alacritty.yaml"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}
		if m := alacrittyFontDottedRE.FindSubmatch(data); len(m) > 1 {
			return strings.TrimSpace(string(m[1]))
		}
		if m := alacrittyFontFlatRE.FindSubmatch(data); len(m) > 1 {
			return strings.TrimSpace(string(m[1]))
		}
	}
	return ""
}

func fontDefFromFontConfig(family string) (fontDef, bool) {
	family = strings.TrimSpace(family)
	if family == "" {
		return fontDef{}, false
	}
	cmd := exec.Command("fc-list", "--format=%{file}\t%{family}\t%{style}\n")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return fontDef{}, false
	}
	def := fontDef{Name: family}
	familyLower := strings.ToLower(family)

	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		file := strings.TrimSpace(parts[0])
		families := strings.Split(parts[1], ",")
		style := strings.ToLower(parts[2])

		matched := false
		for _, f := range families {
			if strings.EqualFold(strings.TrimSpace(f), familyLower) || strings.EqualFold(strings.TrimSpace(f), family) {
				matched = true
				break
			}
		}
		if !matched {
			for _, f := range families {
				if strings.Contains(strings.ToLower(strings.TrimSpace(f)), familyLower) {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}

		switch {
		case strings.Contains(style, "bold") && (strings.Contains(style, "italic") || strings.Contains(style, "oblique")):
			def.BoldItalic = append(def.BoldItalic, file)
		case strings.Contains(style, "bold"):
			def.Bold = append(def.Bold, file)
		case strings.Contains(style, "italic") || strings.Contains(style, "oblique"):
			def.Italic = append(def.Italic, file)
		default:
			def.Regular = append(def.Regular, file)
		}
	}
	if len(def.Regular) == 0 && len(def.Bold) == 0 && len(def.Italic) == 0 && len(def.BoldItalic) == 0 {
		return fontDef{}, false
	}
	return def, true
}

type compactTheme struct {
	dark        bool
	fontSize    float32
	boldAll     bool
	compactMode bool
	fonts       map[string]fontSet
	curFamily   string
}

func newCompactTheme() *compactTheme {
	t := &compactTheme{
		dark:     true,
		fontSize: 11,
		fonts:    make(map[string]fontSet),
	}
	fontDefs := append([]fontDef{}, knownFonts...)
	alacrittyFamily := detectAlacrittyFontFamily()
	if alacrittyDef, ok := fontDefFromFontConfig(alacrittyFamily); ok {
		fontDefs = append(fontDefs, alacrittyDef)
	}
	for _, def := range fontDefs {
		if def.Name == "Default" {
			t.fonts["Default"] = fontSet{}
			continue
		}
		regular := loadFirstFont(def.Regular)
		if regular == nil {
			continue
		}
		t.fonts[def.Name] = fontSet{
			regular:    regular,
			bold:       loadFirstFont(def.Bold),
			italic:     loadFirstFont(def.Italic),
			boldItalic: loadFirstFont(def.BoldItalic),
		}
	}
	if alacrittyFamily != "" {
		if _, ok := t.fonts[alacrittyFamily]; ok {
			t.curFamily = alacrittyFamily
			return t
		}
	}
	if _, ok := t.fonts["Lato"]; ok {
		t.curFamily = "Lato"
	} else {
		t.curFamily = "Default"
	}
	return t
}

func (t *compactTheme) availableFamilies() []string {
	out := make([]string, 0, len(t.fonts))
	for name := range t.fonts {
		if name == "Default" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	out = append([]string{"Default"}, out...)
	return out
}

func (t *compactTheme) base() fyne.Theme {
	if t.dark {
		return theme.DarkTheme()
	}
	return theme.LightTheme()
}

func (t *compactTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if t.dark {
		switch name {
		case theme.ColorNameBackground:
			return color.NRGBA{R: 26, G: 27, B: 38, A: 255}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 26, G: 27, B: 38, A: 255}
		case theme.ColorNameInputBorder:
			return color.NRGBA{A: 0}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 169, G: 177, B: 214, A: 255}
		case theme.ColorNameSuccess:
			return color.NRGBA{R: 148, G: 166, B: 150, A: 255}
		case theme.ColorNameSeparator:
			return color.NRGBA{A: 0}
		}
	}
	if !t.dark {
		switch name {
		case theme.ColorNameBackground:
			return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		case theme.ColorNameInputBorder:
			return color.NRGBA{A: 0}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 15, G: 15, B: 15, A: 255}
		case theme.ColorNameSuccess:
			return color.NRGBA{R: 100, G: 100, B: 100, A: 255}
		case theme.ColorNameSeparator:
			return color.NRGBA{A: 0}
		}
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
	case theme.SizeNamePadding, theme.SizeNameInnerPadding:
		if t.compactMode {
			return 1
		}
		return 2
	case theme.SizeNameInputRadius:
		return 0
	case theme.SizeNameInputBorder:
		return 2
	case theme.SizeNameText, theme.SizeNameSubHeadingText:
		return t.fontSize
	case theme.SizeNameCaptionText:
		if t.compactMode {
			sz := t.fontSize - 2
			if sz < 8 {
				return 8
			}
			return sz
		}
		sz := t.fontSize - 1
		if sz < 8 {
			return 8
		}
		return sz
	default:
		return t.base().Size(name)
	}
}
