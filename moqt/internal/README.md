# `moqt/internal`

## Overview

This directory contains **internal implementation details** used by the public
`moqt` package. It is intentionally split into subpackages, not a single
top-level `internal` Go package.

Current layout:

- `message`: binary message/stream-type codecs used by MOQT control/data paths
- `quicgo`: adapter layer from `quic-go` types to `transport` interfaces
- `webtransportgo`: adapter layer from `okdaichi/webtransport-go` types to
  `transport` interfaces

Because this code is under Go's `internal` directory, it is available only to
code within the module tree and is not part of the external API contract.

## Subpackages

### `message`

Implements low-level wire helpers and message types used by the higher-level
`moqt` package.

- Varint/length-prefixed helpers (`Read*`, `Write*`, `*Len`)
- Stream type codec (`StreamTypeSession`, `StreamTypeAnnounce`,
  `StreamTypeSubscribe`, `StreamTypeGroup`)
- Message structs and encode/decode logic, including:
  - `AnnounceMessage`
  - `AnnounceInterestMessage`
  - `SubscribeMessage`
  - `SubscribeOkMessage`
  - `SubscribeUpdateMessage`
  - `SubscribeDropMessage`
  - `FetchMessage`
  - `ProbeMessage`
  - `GoawayMessage`
  - `GroupMessage`

### `quicgo`

Wraps `github.com/quic-go/quic-go` connections/streams/listeners behind the
project's `transport` interfaces.

Main responsibilities:

- Dial/listen helpers (`DialAddrEarly`, `ListenAddrEarly`)
- Connection wrapper (`WrapConnection`) for `transport.StreamConn`
- Stream wrappers for `transport.Stream`, `transport.SendStream`, and
  `transport.ReceiveStream`

### `webtransportgo`

Wraps `github.com/okdaichi/webtransport-go` sessions/streams so the rest of the
code can keep using `transport` interfaces regardless of backend transport.

Main responsibilities:

- Client dialer wrapper (`Dialer.Dial`)
- Server/upgrader wrappers (`Server`, `Upgrader`)
- Session/stream wrappers to `transport.StreamConn` and related stream
  interfaces

## Relationship to `moqt`

The public API and session behavior live in `moqt/*.go`. Those files import
`moqt/internal/...` to:

- encode/decode MOQT messages,
- abstract over QUIC vs WebTransport connection backends,
- keep backend-specific dependencies out of the public API surface.

## Testing

Each subpackage is tested with unit tests in place:

- `moqt/internal/message/*_test.go`
- `moqt/internal/quicgo/*_test.go`
- `moqt/internal/webtransportgo/*_test.go`

Project-wide tests are run from repository root (already covered by CI/local
`go test ./...`).

## Notes for maintainers

- Treat these packages as implementation details; avoid exposing their types in
  exported public APIs.
- Keep wrapper behavior aligned with `transport` interface contracts.
- When adding wire-level messages, update both encode/decode paths and tests in
  `message`.
