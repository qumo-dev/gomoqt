//go:build windows

package quicgo

import "golang.org/x/sys/windows"

// netChangeErrnos are the errors a send fails with while the OS has no route
// to the peer for now, as while a laptop moves between networks.
var netChangeErrnos = []error{
	windows.WSAENETUNREACH,
	windows.WSAEHOSTUNREACH,
	windows.WSAENETDOWN,
	windows.WSAEADDRNOTAVAIL,
	windows.WSAENOBUFS,
}
