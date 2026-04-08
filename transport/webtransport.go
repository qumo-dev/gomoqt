package transport

type WebTransportSession interface {
	StreamConn
	Subprotocol() string
}
