//go:build !windows

package tui

import (
	"os"
	"strconv"
	"strings"
)

// NativeConsole is the minimal non-Windows fallback. Windows x64 remains the
// only platform currently declared and independently tested for distribution.
type NativeConsole struct {
	Input  *os.File
	Output *os.File
}

func (c NativeConsole) files() (*os.File, *os.File) {
	in, out := c.Input, c.Output
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return in, out
}

func (c NativeConsole) Prepare() (ConsoleInfo, func() error, error) {
	in, out := c.files()
	interactive := isCharacterDevice(in) && isCharacterDevice(out)
	ansi := interactive && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == ""
	level := 0
	if ansi {
		level = detectColorLevel()
	}
	return ConsoleInfo{Interactive: interactive, ANSI: ansi, ColorLevel: level}, func() error { return nil }, nil
}

func (c NativeConsole) Size() Size {
	width, height := envDimension("COLUMNS", 80), envDimension("LINES", 24)
	return Size{Width: width, Height: height}
}

func (c NativeConsole) DiscardPendingInput() error { return nil }

// IsInteractive reports whether both streams refer to character devices.
func IsInteractive(input, output *os.File) bool {
	return isCharacterDevice(input) && isCharacterDevice(output)
}

func isCharacterDevice(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func envDimension(name string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}
	return fallback
}

func detectColorLevel() int {
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	if colorTerm == "truecolor" || colorTerm == "24bit" {
		return 24
	}
	if strings.Contains(strings.ToLower(os.Getenv("TERM")), "256color") {
		return 256
	}
	return 16
}
