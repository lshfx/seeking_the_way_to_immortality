//go:build windows

package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	enableVirtualTerminalProcessing = 0x0004
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode       = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode       = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procFlushConsoleInput    = kernel32.NewProc("FlushConsoleInputBuffer")
	procGetInputEventCount   = kernel32.NewProc("GetNumberOfConsoleInputEvents")
)

type nativeCoord struct{ X, Y int16 }
type nativeSmallRect struct{ Left, Top, Right, Bottom int16 }
type nativeConsoleInfo struct {
	Size              nativeCoord
	CursorPosition    nativeCoord
	Attributes        uint16
	Window            nativeSmallRect
	MaximumWindowSize nativeCoord
}

// NativeConsole enables only ANSI output processing. It leaves console input
// in its original line/echo mode, so ordinary exit needs no raw-mode repair.
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
	if !interactive {
		return ConsoleInfo{}, func() error { return nil }, nil
	}
	var pending uint32
	result, _, callErr := procGetInputEventCount.Call(uintptr(syscall.Handle(in.Fd())), uintptr(unsafe.Pointer(&pending)))
	if result == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return ConsoleInfo{}, nil, fmt.Errorf("Windows console input queue is unavailable: %w", errno)
		}
		return ConsoleInfo{}, nil, errors.New("Windows console input queue is unavailable")
	}
	handle := syscall.Handle(out.Fd())
	var original uint32
	result, _, _ = procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&original)))
	if result == 0 {
		return ConsoleInfo{Interactive: true}, func() error { return nil }, nil
	}
	if original&enableVirtualTerminalProcessing == 0 {
		result, _, callErr := procSetConsoleMode.Call(uintptr(handle), uintptr(original|enableVirtualTerminalProcessing))
		if result == 0 {
			// A console that refuses VT output still receives plain, readable
			// text. No control sequences are written in this fallback.
			_ = callErr
			return ConsoleInfo{Interactive: true}, func() error { return nil }, nil
		}
	}
	return ConsoleInfo{Interactive: true, ANSI: true, ColorLevel: detectColorLevel()}, func() error {
		result, _, callErr := procSetConsoleMode.Call(uintptr(handle), uintptr(original))
		if result == 0 {
			return callErr
		}
		return nil
	}, nil
}

func (c NativeConsole) Size() Size {
	_, out := c.files()
	var info nativeConsoleInfo
	result, _, _ := procGetConsoleScreenInfo.Call(uintptr(syscall.Handle(out.Fd())), uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return Size{Width: 80, Height: 24}
	}
	return Size{
		Width:  int(info.Window.Right-info.Window.Left) + 1,
		Height: int(info.Window.Bottom-info.Window.Top) + 1,
	}
}

func (c NativeConsole) DiscardPendingInput() error {
	in, _ := c.files()
	handle := syscall.Handle(in.Fd())
	var count uint32
	result, _, callErr := procGetInputEventCount.Call(uintptr(handle), uintptr(unsafe.Pointer(&count)))
	if result == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return fmt.Errorf("GetNumberOfConsoleInputEvents: %w", errno)
		}
		return errors.New("GetNumberOfConsoleInputEvents failed")
	}
	if count == 0 {
		return nil
	}
	result, _, callErr = procFlushConsoleInput.Call(uintptr(handle))
	if result == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return fmt.Errorf("FlushConsoleInputBuffer: %w", errno)
		}
		return errors.New("FlushConsoleInputBuffer failed")
	}
	return nil
}

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

func detectColorLevel() int {
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	if colorTerm == "truecolor" || colorTerm == "24bit" || os.Getenv("WT_SESSION") != "" {
		return 24
	}
	if strings.Contains(strings.ToLower(os.Getenv("TERM")), "256color") {
		return 256
	}
	return 16
}
