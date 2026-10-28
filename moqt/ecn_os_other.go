//go:build !darwin && !linux && !freebsd

package moqt

// ecnOS reports whether quic-go marks ECN on this OS: it does not here, its
// sockets all being a basicConn (sys_conn_windows.go, sys_conn_no_oob.go).
const ecnOS = false
