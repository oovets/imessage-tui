package tui

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points HOME at a throwaway directory for the whole package. The app
// saves its layout, UI state and chat overrides under ~/.config/imessage-tui
// as a side effect of ordinary key presses and clicks, so without this a test
// that presses ctrl+b or selects a chat writes over the real files of whoever
// runs the suite.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "imessage-tui-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
