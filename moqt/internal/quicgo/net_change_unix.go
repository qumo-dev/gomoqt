//go:build unix && !android

package quicgo

import "syscall"

// netChangeErrnos are the errors a send fails with while the OS has no route
// to the peer for now: no route to the network or host, the network down,
// the interface's address gone (macOS and iOS), or no buffer space while the
// interface is torn down.
var netChangeErrnos = []error{
	syscall.ENETUNREACH,
	syscall.EHOSTUNREACH,
	syscall.ENETDOWN,
	syscall.EADDRNOTAVAIL,
	syscall.ENOBUFS,
}
