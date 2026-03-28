package moqt

// NextProtoMOQ is the default ALPN token for MOQ over QUIC.
//
// moq-lite-03 negotiates via ALPN token "moq-lite-03" for native QUIC.
const NextProtoMOQ = "moq-lite-03"

// NextProtoH3 is the ALPN token used to indicate HTTP/3 (used for WebTransport).
const NextProtoH3 = "h3"
