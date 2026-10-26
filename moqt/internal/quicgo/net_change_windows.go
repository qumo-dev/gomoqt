//go:build windows

package quicgo

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isNetChangeErr reports whether a send failed because the OS has no route
// to the peer for now, as while a laptop moves between networks.
func isNetChangeErr(err error) bool {
	return errors.Is(err, windows.WSAENETUNREACH) ||
		errors.Is(err, windows.WSAEHOSTUNREACH) ||
		errors.Is(err, windows.WSAENETDOWN)
}
