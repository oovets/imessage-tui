package gui

import (
	"log"
	"os"
	"strings"
	"time"
)

var perfEnabled = strings.TrimSpace(strings.ToLower(os.Getenv("BB_GUI_PERF"))) == "1"

func perfStart(name string) func() {
	if !perfEnabled {
		return func() {}
	}
	start := time.Now()
	return func() {
		ms := float64(time.Since(start).Microseconds()) / 1000.0
		log.Printf("[GUI PERF] %s %.2fms", name, ms)
	}
}
