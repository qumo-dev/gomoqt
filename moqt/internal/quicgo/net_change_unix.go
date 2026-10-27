//go:build unix

package quicgo

import (
	"errors"
	"runtime"
	"syscall"
)

// isNetChangeErr reports whether a send failed because the OS has no route
// to the peer for now, as while a phone moves between Wi-Fi and cellular.
//
// On Linux, which Android is, that includes EPERM: Android refuses a send
// with it on a socket bound to a network being torn down, and for an app
// whose network access is blocked for now, as while its screen is off.
func isNetChangeErr(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETDOWN) ||
		(runtime.GOOS == "linux" || runtime.GOOS == "android") && errors.Is(err, syscall.EPERM)
}
