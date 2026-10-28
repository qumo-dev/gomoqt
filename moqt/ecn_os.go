//go:build darwin || linux || freebsd

package moqt

// ecnOS reports whether quic-go marks ECN on this OS: it does on darwin,
// linux and freebsd, iOS and Android among them, through sys_conn_oob.go.
const ecnOS = true
