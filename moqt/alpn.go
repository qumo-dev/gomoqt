package moqt

// NextProtoMOQ is the default ALPN token for MOQ over QUIC.
//
// moq-lite-04 negotiates via ALPN token "moq-lite-04" for native QUIC.
const NextProtoMOQ = "moq-lite-04"

// NextProtoH3 is the ALPN token used to indicate HTTP/3 (used for WebTransport).
const NextProtoH3 = "h3"
