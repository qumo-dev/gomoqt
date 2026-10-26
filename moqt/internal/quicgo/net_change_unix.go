//go:build unix

package quicgo

import (
	"errors"
	"syscall"
)

// isNetChangeErr reports whether a send failed because the OS has no route
// to the peer for now, as while a phone moves between Wi-Fi and cellular.
func isNetChangeErr(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETDOWN)
}
