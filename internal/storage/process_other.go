//go:build !windows

package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// processExistsPlatform reports whether a pid exists.
//
// Signal 0 performs the permission and existence checks without delivering a
// signal, which is the portable way to ask "is this pid alive". ESRCH means
// gone; EPERM means it exists but belongs to another user, which is reported as
// present because that is the safe direction.
//
// This file exists so the package compiles and its tests run off Windows. That
// is a development convenience, not a support claim: the only supported target
// is Windows.
func processExistsPlatform(pid int) (bool, error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

// processStartUnixNanoPlatform reads a process's start time from /proc, which
// records it in clock ticks since boot. Converting needs the system boot time
// and the tick rate, so the two are combined with the field's own value.
//
// It returns 0 when the information is unavailable, which callers treat as a
// reason to refuse a takeover rather than to guess.
func processStartUnixNanoPlatform(pid int) int64 {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	// The second field is the command name in parentheses and may itself
	// contain spaces and parentheses, so parsing starts after the LAST ')'.
	text := string(stat)
	close := strings.LastIndex(text, ")")
	if close < 0 || close+2 >= len(text) {
		return 0
	}
	fields := strings.Fields(text[close+2:])
	// Field 22 overall is starttime, which is index 19 after the two consumed
	// fields (pid and comm).
	if len(fields) < 20 {
		return 0
	}
	startTicks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0
	}

	ticksPerSecond := int64(100) // USER_HZ is 100 on every mainstream Linux
	boot := bootTimeUnixNano()
	if boot == 0 {
		return 0
	}
	return boot + startTicks*int64(1e9)/ticksPerSecond
}

// bootTimeUnixNano derives the boot instant from /proc/stat's btime line.
func bootTimeUnixNano() int64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		secs, err := strconv.ParseInt(strings.TrimSpace(line[len("btime "):]), 10, 64)
		if err != nil {
			return 0
		}
		return secs * int64(1e9)
	}
	return 0
}
