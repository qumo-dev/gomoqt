package moqt

// NextProtoMOQ is the default ALPN token for MOQ over QUIC.
//
// moq-lite-03 negotiates via ALPN token "moq-lite-03" for native QUIC.
const NextProtoMOQ = "moq-lite-03"

// NextProtoH3 is the ALPN token used to indicate HTTP/3 (used for WebTransport).
const NextProtoH3 = "h3"

// SupportedNativeMOQALPNs returns native MOQ ALPN tokens in preference order.
//
// This is a global variable for convenience and can be used directly in configs.
var SupportedNativeMOQALPNs = []string{NextProtoMOQ}

// SupportedWebTransportMOQProtocols returns supported moq-lite protocol tokens
// for WebTransport negotiation headers.
//
// This is a global variable to simplify common usage.
var SupportedWebTransportMOQProtocols = SupportedNativeMOQALPNs

// DefaultServerNextProtos is the default NextProtos list used by servers.
var DefaultServerNextProtos = []string{NextProtoH3, NextProtoMOQ}
