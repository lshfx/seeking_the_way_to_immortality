//go:build windows

package storage

import (
	"syscall"
	"unsafe"
)

// Windows process access rights and constants used for liveness checks.
const (
	// processQueryLimitedInformation is enough to read the creation time and is
	// far less privileged than PROCESS_QUERY_INFORMATION, so it works against
	// processes owned by other users.
	processQueryLimitedInformation = 0x1000

	// stillActive is the STILL_ACTIVE exit code returned by GetExitCodeProcess
	// for a running process.
	stillActive = 259

	// invalidHandleValue is what OpenProcess returns on failure.
	invalidHandleValue = ^uintptr(0)

	// errorAccessDenied and errorInvalidParameter are the two OpenProcess
	// failures that matter. They are declared locally rather than taken from
	// syscall because the syscall package does not export every Errno constant
	// on every platform, and a build that fails on a missing constant is worse
	// than a build with two documented literals.
	errorAccessDenied     = syscall.Errno(5)
	errorInvalidParameter = syscall.Errno(87)
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess             = kernel32.NewProc("OpenProcess")
	procCloseHandle             = kernel32.NewProc("CloseHandle")
	procGetExitCodeProcess      = kernel32.NewProc("GetExitCodeProcess")
	procGetProcessTimes         = kernel32.NewProc("GetProcessTimes")
	procGetSystemTimeAsFileTime = kernel32.NewProc("GetSystemTimeAsFileTime")
)

// windowsFiletimeToUnixNano converts a Windows FILETIME (100-nanosecond
// intervals since 1601-01-01 UTC) into unix nanoseconds.
//
// The epoch offset is the number of 100ns intervals between 1601-01-01 and
// 1970-01-01. Getting this constant wrong would shift every start time equally
// and stay self-consistent, so it is asserted in a test against a known-good
// conversion rather than trusted.
const (
	filetimeEpochOffset100ns = 116444736000000000
	filetimeTicksPerSecond   = 10000000
)

func filetimeToUnixNano(ft uint64) int64 {
	if ft < filetimeEpochOffset100ns {
		return 0
	}
	return int64((ft - filetimeEpochOffset100ns) * 100)
}

// processExistsPlatform reports whether a pid is present.
//
// OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION succeeds for any live
// process the caller may query, including those owned by another user. A
// failure is ambiguous between "gone" and "not permitted", so the caller
// distinguishes them through lastError rather than assuming absence.
func processExistsPlatform(pid int) (bool, error) {
	h, _, err := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 || h == invalidHandleValue {
		// ERROR_INVALID_PARAMETER (87) or ERROR_ACCESS_DENIED (5).
		//
		// Access denied means the process exists but belongs to another user;
		// treating that as absent would be the dangerous direction, so it is
		// reported as present.
		if errno, ok := err.(syscall.Errno); ok {
			switch errno {
			case errorAccessDenied:
				return true, nil
			case errorInvalidParameter:
				return false, nil
			}
		}
		return false, nil
	}
	defer procCloseHandle.Call(h)

	var code uint32
	ret, _, callErr := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code)))
	if ret == 0 {
		return true, callErr
	}
	return code == stillActive, nil
}

// processStartUnixNanoPlatform reads a process's creation time.
//
// The timestamp is local-time-independent (GetProcessTimes returns UTC), so no
// timezone handling is involved. Returning 0 means "cannot determine", which
// callers treat as a reason to refuse rather than to guess.
func processStartUnixNanoPlatform(pid int) int64 {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 || h == invalidHandleValue {
		return 0
	}
	defer procCloseHandle.Call(h)

	var creation, exit, kernel, user uint64
	ret, _, _ := procGetProcessTimes.Call(
		h,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		return 0
	}
	return filetimeToUnixNano(creation)
}

// systemTimeAsUnixNano returns the current UTC time via the same clock that
// produced process start times, so a comparison between the two is not skewed
// by a different time source. Tests use it to sanity-check conversions.
func systemTimeAsUnixNano() int64 {
	var ft uint64
	procGetSystemTimeAsFileTime.Call(uintptr(unsafe.Pointer(&ft)))
	return filetimeToUnixNano(ft)
}
