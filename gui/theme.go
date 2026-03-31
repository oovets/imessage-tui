package gui

import (
	_ "embed"
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

//go:embed fonts/Inter-Regular.ttf
var interRegular []byte

//go:embed fonts/Inter-Bold.ttf
var interBold []byte

//go:embed fonts/Inter-Italic.ttf
var interItalic []byte

//go:embed fonts/Inter-BoldItalic.ttf
var interBoldItalic []byte

//go:embed fonts/Lato-Regular.ttf
var latoRegular []byte

//go:embed fonts/Lato-Bold.ttf
var latoBold []byte

//go:embed fonts/Lato-Italic.ttf
var latoItalic []byte

//go:embed fonts/Lato-BoldItalic.ttf
var latoBoldItalic []byte

//go:embed fonts/NotoSans-Regular.ttf
var notoSansRegular []byte

//go:embed fonts/NotoSans-Bold.ttf
var notoSansBold []byte

//go:embed fonts/NotoSans-Italic.ttf
var notoSansItalic []byte

//go:embed fonts/NotoSans-BoldItalic.ttf
var notoSansBoldItalic []byte

//go:embed fonts/JetBrainsMono-Regular.ttf
var jetBrainsMonoRegular []byte

//go:embed fonts/JetBrainsMono-Bold.ttf
var jetBrainsMonoBold []byte

//go:embed fonts/JetBrainsMono-Italic.ttf
var jetBrainsMonoItalic []byte

//go:embed fonts/JetBrainsMono-BoldItalic.ttf
var jetBrainsMonoBoldItalic []byte

var bundledFonts = map[string]fontSet{
	"Inter": {
		regular:    fyne.NewStaticResource("Inter-Regular.ttf", interRegular),
		bold:       fyne.NewStaticResource("Inter-Bold.ttf", interBold),
		italic:     fyne.NewStaticResource("Inter-Italic.ttf", interItalic),
		boldItalic: fyne.NewStaticResource("Inter-BoldItalic.ttf", interBoldItalic),
	},
	"Lato": {
		regular:    fyne.NewStaticResource("Lato-Regular.ttf", latoRegular),
		bold:       fyne.NewStaticResource("Lato-Bold.ttf", latoBold),
		italic:     fyne.NewStaticResource("Lato-Italic.ttf", latoItalic),
		boldItalic: fyne.NewStaticResource("Lato-BoldItalic.ttf", latoBoldItalic),
	},
	"Noto Sans": {
		regular:    fyne.NewStaticResource("NotoSans-Regular.ttf", notoSansRegular),
		bold:       fyne.NewStaticResource("NotoSans-Bold.ttf", notoSansBold),
		italic:     fyne.NewStaticResource("NotoSans-Italic.ttf", notoSansItalic),
		boldItalic: fyne.NewStaticResource("NotoSans-BoldItalic.ttf", notoSansBoldItalic),
	},
	"JetBrains Mono": {
		regular:    fyne.NewStaticResource("JetBrainsMono-Regular.ttf", jetBrainsMonoRegular),
		bold:       fyne.NewStaticResource("JetBrainsMono-Bold.ttf", jetBrainsMonoBold),
		italic:     fyne.NewStaticResource("JetBrainsMono-Italic.ttf", jetBrainsMonoItalic),
		boldItalic: fyne.NewStaticResource("JetBrainsMono-BoldItalic.ttf", jetBrainsMonoBoldItalic),
	},
}

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

// compactTheme is a mutable theme supporting dark/light mode, font size, font family, and bold.
type compactTheme struct {
	dark        bool
	fontSize    float32
	boldAll     bool
	compactMode bool
	fonts       map[string]fontSet
	curFamily   string
}

const (
	minUIFontSize = 6
	maxUIFontSize = 20
)

func newCompactTheme() *compactTheme {
	t := &compactTheme{
		dark:     true,
		fontSize: 11,
		fonts:    make(map[string]fontSet),
	}

	// Always available: bundled fonts
	t.fonts["Default"] = fontSet{}
	for name, fs := range bundledFonts {
		t.fonts[name] = fs
	}

	// System fonts: add with "(system)" suffix if name already bundled
	fontDefs := append([]fontDef{}, knownFonts...)
	alacrittyFamily := detectAlacrittyFontFamily()
	if alacrittyDef, ok := fontDefFromFontConfig(alacrittyFamily); ok {
		fontDefs = append(fontDefs, alacrittyDef)
	}
	for _, def := range fontDefs {
		if def.Name == "Default" {
			continue
		}
		regular := loadFirstFont(def.Regular)
		if regular == nil {
			continue
		}
		name := def.Name
		if _, exists := bundledFonts[name]; exists {
			name = name + " (system)"
		}
		t.fonts[name] = fontSet{
			regular:    regular,
			bold:       loadFirstFont(def.Bold),
			italic:     loadFirstFont(def.Italic),
			boldItalic: loadFirstFont(def.BoldItalic),
		}
	}

	// Prefer user's Alacritty family when available, otherwise default to Inter.
	if alacrittyFamily != "" {
		if _, ok := t.fonts[alacrittyFamily]; ok {
			t.curFamily = alacrittyFamily
			return t
		}
	}
	t.curFamily = "Inter"
	return t
}

// availableFamilies returns the names of installed font families in display order.
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
			return color.NRGBA{R: 23, G: 24, B: 33, A: 255}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 31, G: 33, B: 45, A: 255}
		case theme.ColorNameInputBorder:
			return color.NRGBA{R: 84, G: 93, B: 122, A: 160}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 186, G: 192, B: 222, A: 255}
		case theme.ColorNameSuccess:
			return color.NRGBA{R: 150, G: 154, B: 168, A: 255}
		case theme.ColorNamePrimary:
			return color.NRGBA{R: 109, G: 133, B: 214, A: 220}
		case theme.ColorNameSeparator:
			return color.NRGBA{A: 0}
		case theme.ColorNameShadow:
			return color.NRGBA{A: 0}
		}
	}
	if !t.dark {
		switch name {
		case theme.ColorNameBackground:
			return color.NRGBA{R: 248, G: 249, B: 252, A: 255}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		case theme.ColorNameInputBorder:
			return color.NRGBA{R: 189, G: 196, B: 214, A: 220}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 22, G: 25, B: 33, A: 255}
		case theme.ColorNameSuccess:
			return color.NRGBA{R: 108, G: 114, B: 126, A: 255}
		case theme.ColorNamePrimary:
			return color.NRGBA{R: 67, G: 96, B: 165, A: 255}
		case theme.ColorNameSeparator:
			return color.NRGBA{A: 0}
		case theme.ColorNameShadow:
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
	case theme.SizeNamePadding:
		if t.compactMode {
			return 2
		}
		return 6
	case theme.SizeNameInnerPadding:
		if t.compactMode {
			return 1
		}
		return 4
	case theme.SizeNameInputRadius:
		if t.compactMode {
			return 4
		}
		return 8
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameScrollBar, theme.SizeNameScrollBarSmall:
		return 0
	case theme.SizeNameText, theme.SizeNameSubHeadingText:
		return t.fontSize
	case theme.SizeNameCaptionText:
		if t.compactMode {
			sz := t.fontSize - 2
			if sz < minUIFontSize {
				return minUIFontSize
			}
			return sz
		}
		sz := t.fontSize - 1
		if sz < minUIFontSize {
			return minUIFontSize
		}
		return sz
	default:
		return t.base().Size(name)
	}
}
