package termqr

import (
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderFoldsTwoModuleRowsIntoOneLine(t *testing.T) {
	// A 2×2 code: top-left and bottom-right dark.
	pattern := [2][2]bool{{true, false}, {false, true}}
	got := ansi.ReplaceAllString(render(2, func(x, y int) bool { return pattern[y][x] }), "")

	blank := strings.Repeat(" ", 2+2*quietZone)
	want := strings.Join([]string{
		blank,
		"  ▀▄  ",
		blank,
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("render =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderEncodesAURL(t *testing.T) {
	out, err := Render("tg://login?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(ansi.ReplaceAllString(out, ""), "\n"), "\n")
	if len(lines) < 10 {
		t.Fatalf("got %d lines, want a full code", len(lines))
	}
	for i, line := range lines {
		if got, want := len([]rune(line)), len([]rune(lines[0])); got != want {
			t.Fatalf("line %d is %d columns, want %d", i, got, want)
		}
	}
}
