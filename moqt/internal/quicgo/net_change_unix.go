//go:build unix

package quicgo

import "syscall"

// netChangeErrnos are the errors a send fails with while the OS has no route
// to the peer for now, as while a phone moves between Wi-Fi and cellular: no
// route to the network or host, or the network down.
var netChangeErrnos = []error{
	syscall.ENETUNREACH,
	syscall.EHOSTUNREACH,
	syscall.ENETDOWN,
}
