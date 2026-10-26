//go:build !unix && !windows

package quicgo

// isNetChangeErr reports no error as a network change where the OS's send
// errors for one are not known: a failed send closes the connection, as in
// quic-go.
func isNetChangeErr(error) bool { return false }
