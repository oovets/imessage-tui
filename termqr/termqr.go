// Package termqr draws a QR code as text, for the login subcommands that run
// before the TUI starts. Telegram's QR login and Signal's device linking both
// hand over a URL that a phone has to scan, and printing it as half-block
// characters needs no image protocol, so it works in any terminal and over
// ssh.
package termqr

import (
	"strings"

	"rsc.io/qr"
)

// quietZone is the light border a scanner needs to find the code. The spec
// asks for four modules; two is what phone cameras reliably manage with and
// keeps the code on a small terminal.
const quietZone = 2

// Render returns text drawn as a QR code, one terminal row per two module
// rows.
//
// The colours are explicit — black on white — rather than whatever the
// terminal's own are: on a dark theme the default would draw an inverted code,
// which some scanners refuse.
func Render(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	return render(code.Size, code.Black), nil
}

func render(size int, black func(x, y int) bool) string {
	dark := func(x, y int) bool {
		x, y = x-quietZone, y-quietZone
		if x < 0 || y < 0 || x >= size || y >= size {
			return false
		}
		return black(x, y)
	}

	const (
		on  = "\x1b[30;47m" // black on white
		off = "\x1b[0m"
	)
	full := size + 2*quietZone
	var b strings.Builder
	for y := 0; y < full; y += 2 {
		b.WriteString(on)
		for x := 0; x < full; x++ {
			top, bottom := dark(x, y), dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString(off)
		b.WriteString("\n")
	}
	return b.String()
}
