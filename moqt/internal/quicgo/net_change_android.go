package quicgo

import "syscall"

// netChangeErrnos are the errors a send fails with while the OS has no route
// to the peer for now, as on other Unix systems, and EPERM: Android refuses
// a send with it on a socket bound to a network being torn down, and for an
// app whose network access is blocked for now, as while its screen is off.
// Elsewhere on Linux EPERM is a firewall's lasting refusal, so it is not
// listed there.
var netChangeErrnos = []error{
	syscall.ENETUNREACH,
	syscall.EHOSTUNREACH,
	syscall.ENETDOWN,
	syscall.EADDRNOTAVAIL,
	syscall.ENOBUFS,
	syscall.EPERM,
}
