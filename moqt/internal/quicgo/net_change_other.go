//go:build !unix && !windows

package quicgo

// netChangeErrnos is empty where the OS's send errors for a network change
// are not known: a failed send closes the connection, as in quic-go.
var netChangeErrnos []error
