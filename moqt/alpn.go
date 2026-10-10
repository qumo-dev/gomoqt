package moqt

import "github.com/qumo-dev/gomoqt/moqt/internal/qmux"

// NextProtoMOQ is the default ALPN token for MOQ over QUIC.
//
// moq-lite-05 negotiates via ALPN token "moq-lite-05" for native QUIC.
const NextProtoMOQ = "moq-lite-05"

// NextProtoQMux is the WebSocket subprotocol for MOQ over QMux: the QMux
// draft, then the application protocol. A WebSocket has no ALPN, so the
// subprotocol is what the two ends agree on.
const NextProtoQMux = qmux.Version + "." + NextProtoMOQ

// NextProtoH3 is the ALPN token used to indicate HTTP/3 (used for WebTransport).
const NextProtoH3 = "h3"
