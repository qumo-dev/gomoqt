# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **moqt:** `Session.Stats()` returns `SessionStats` with estimated bitrate, transport RTT, bytes sent, and bytes received.

## [v0.14.0] - 2026-04-23

### Added

- **moqt:** `Session.ProbeTargets() <-chan ProbeResult` — publisher-side channel for the latest subscriber target bitrate (buffered 1, latest-value semantics).
- **moq-web:** `Session.probe()` support with `ProbeResult` handling for bitrate measurement.

### Changed

- Repository owner changed from `okdaichi` to `qumo-dev`.
- **moqt:** `Session.Probe()` reuses the same stream on repeated calls; the channel is closed when the stream or session ends.
- **moqt:** `ProbeResult.RTT` removed; RTT is available via the underlying transport API.
- **moqt:** Publisher now enforces a single active incoming probe stream; a new stream cancels the previous one.

### Fixed

- **moqt:** Fixed probe stream cleanup on session or stream close.

## [v0.13.4] - 2026-04-20

### Fixed

- **moqt:** Republished as `v0.13.4` to ensure Go module consumers can resolve the correct immutable version after the `v0.13.3` tag was moved.

## [v0.13.3] - 2026-04-20

### Changed

- **moqt:** Removed the unused `path` argument from `Dialer.DialQUIC()`. The native QUIC dialer now accepts only `addr` and `mux`.

## [v0.13.2] - 2026-04-19

### Added

- **moqt:** `NewWebTransportServer(handler http.Handler)` factory function for creating a `WebTransportServer` with a custom `http.Handler`.

### Fixed

- **moqt:** Fixed `NewHopID()` to always generate identifiers within the 62-bit varint range.

## [v0.13.1] - 2026-04-17

### Fixed

- **moqt:** Injected the WebTransport subprotocol externally and updated draft-04 references in WebTransport dialing.

## [v0.13.0] - 2026-04-16

### Added

- **moqt:** Graceful session migration per draft-04. `Server.NextSessionURI` specifies the redirect URI sent during `Shutdown()`. `Dialer.OnGoaway` delivers the received redirect URI to the client.
- **moqt:** `NewHopID()` for generating cryptographically random non-zero hop identifiers for relay nodes.
- **moqt:** `ProbeResult` struct now includes both `Bitrate` and `RTT` fields.

### Changed

- **moqt:** Upgraded protocol version from `moq-lite-03` to `moq-lite-04` (ALPN `moq-lite-04`).
- **moqt:** `NewTrackMux(id uint64)` now requires a hop ID. Pass `0` for edge nodes; relay nodes should use `NewHopID()`.
- **moqt:** `Session.Probe()` returns `(*ProbeResult, error)` instead of `(uint64, error)`.
- **moqt:** `Announcement.HopIDs() []uint64` replaces `Announcement.Hops() int` — returns an explicit hop ID list per draft-04.
- **moqt:** `AnnouncementWriter` supports `ExcludeHop` filtering and appends the local hop ID when forwarding announcements.

### Removed

- **moqt:** `AnnouncePleaseMessage` superseded by `AnnounceInterestMessage`.

## [v0.12.2] - 2026-04-16

### Added

- **moq-web:** `connect(url, init?)` as the primary API for opening MOQ sessions, following the browser `fetch(url, init?)` pattern.
- **moq-web:** `ConnectInit` interface replacing the positional `mux` argument, with fields: `mux?`, `transportOptions?`, `onGoaway?`, `transportFactory?`.
- **moq-web:** GOAWAY verification step to the TypeScript interop client.

### Changed

- **moq-web:** `Client` class is now `@deprecated`; use `connect()` instead. Kept as a back-compat shim.
- **moq-web:** `ConnectOptions` and `MOQOptions` are now `@deprecated` aliases for `ConnectInit`.
- **moq-web:** `TransportFactory` now returns `WebTransport` (the browser interface); automatically wrapped in `WebTransportSession` inside `connect()`.

## [v0.12.1] - 2026-04-14

### Added

- **docs:** New MSF documentation covering Catalog, Catalog Delta, Timeline, and Broadcast.
- **docs:** New pages for Dialer, Fetch, and Probe.
- **moq-web:** Comprehensive JSDoc documentation for all exported symbols, following JSR conventions.

### Changed

- **docs:** Restructured and updated existing documentation pages to reflect the current API.
- **README:** Added MSF package references to all language translations; removed outdated QUIC wrapper and WebTransport server references.

### Removed

- **docs:** Removed Hang protocol documentation and legacy `client.md` (replaced by `dialer.md`).

## [v0.12.0] - 2026-04-14

### Added

- **moqt:** `ProbeErrorCodeNotSupported` returned when the underlying connection does not support `ConnectionStats()`, allowing clients to distinguish capability absence from implementation errors.
- **moqt:** WebTransport sessions now support `ConnectionStats()`, enabling bitrate measurement for Probe streams over WebTransport.
- **transport:** Shared transport abstraction package with core connection/listener/config/error interfaces.
- **moqt:** Protocol constants `NextProtoMOQ` and `NextProtoH3` for ALPN-based negotiation.
- **moqt:** Top-level transport type aliases (e.g. `moqt.StreamConn`, `moqt.QUICListener`) to reduce `transport.` prefix leakage.
- **moqt:** `NativeQUICHandler` for native MOQ-over-QUIC session handling.

### Changed

- **moqt:** Native QUIC dialer now defaults `TLSConfig.NextProtos` to `moq-lite-03` if unset.
- **Dependencies:** Switched to `github.com/okdaichi/webtransport-go v0.10.2-okdaichi.1`.

### Fixed

- **moqt:** Fixed missing message-type prefix bytes for `SubscribeOk` and `SubscribeDrop` that caused type mismatches when a TypeScript client decoded subscribe responses.
- **moqt:** Fixed premature `CancelRead()` on group streams passed to a `TrackReader`.
- **moqt:** `WebTransportHandler.ServeHTTP` no longer panics when used standalone without `Server`.
- **moqt:** Fixed `Server.ListenAndServe()` and `Server.ListenAndServeTLS()` to correctly use a custom `Server.ListenFunc`.
- **cmd/interop:** Interop server now waits for the client to close the session before tearing down the connection.

### Removed

- **moqt:** Removed legacy `Version` API in favor of ALPN/subprotocol-based negotiation.
- **moqt:** Removed obsolete `Extension` API.
- **moq-web:** Removed obsolete version and extensions negotiation surfaces.

## [v0.11.0] - 2026-03-12

### Changed

- **moq-web/msf:** Adopted `zod` for JSON boundary validation in catalog, catalog delta, and timeline parsing.

## [v0.10.8] - 2026-03-12

### Changed

- **moq-web:** Reorganized MSF code into `src/msf/` with a generic `Broadcast` class and MSF-aware broadcast implementation.
- **moq-web:** Exposed `NotFoundTrackHandler` and added `NotFound` helper for API parity with Go.

## [v0.10.7] - 2026-02-27

### Added

- Non-root `appuser` and workspace ownership in Dockerfile.

### Changed

- Updated README files across all language translations.

## [v0.10.5] - 2026-02-20

### Added

- Allow overriding the interop server address via the `INTEROP_ADDR` environment variable.
- Magefile for build automation and environment setup.

## [v0.10.4] - 2026-02-20

### Fixed

- **moqt:** Enabled `EnableDatagrams` and `EnableStreamResetPartialDelivery` by default in `ListenAndServe()` / `ListenAndServeTLS()` so WebTransport connections work without manual configuration.
- **moqt:** Fixed server shutdown hang caused by a `WaitGroup` leak in `Close()` / `Shutdown()`.
- **webtransport:** Initialized HTTP/3 server pointer in `NewServer()` to avoid a nil-pointer panic with `webtransport-go v0.10.0`.

## [v0.10.2] - 2026-02-10

### Added

- **moq-web:** Validation for maximum varint size in `writeVarint` — returns `RangeError` for `Infinity` or values exceeding `MAX_VARINT8`.

### Changed

- **Dependencies:** Updated `quic-go` to v0.59.0 and `webtransport-go` to v0.10.0.

## [v0.10.1] - 2026-01-04

### Fixed

- **moq-web:** Fixed QUIC varint encoding/decoding for values exceeding 32 bits — JavaScript bitwise operations are limited to 32 bits, causing incorrect encoding of 8-byte varints.

### Changed

- **moq-web:** `GroupWriter.writeFrame()` now accepts `ByteSource | Uint8Array`; `GroupReader.readFrame()` now accepts `ByteSink | ByteSinkFunc`.

## [v0.10.0] - 2026-01-04

### Changed

- **moq-web:** Frame API redesigned with `ByteSource`/`ByteSink` pattern — replaced `bytes` property access with `ByteSource.copyTo()` for safer data handling.

### Fixed

- **moq-web:** Fixed buffer overflow in `Frame.copyTo()` caused by a mismatched internal buffer size.

## [v0.9.0] - 2025-12-24

### Added

- **moqt:** `TrackWriter.OpenGroupAt(seq GroupSequence)` to open a group with an explicit sequence number; the internal counter advances to at least `seq+1` to prevent collisions.

### Changed

- **moqt:** `OpenGroup()` now returns sequences starting from `0`.
- **moqt:** `GroupSequence.Next()` wraps from `MaxGroupSequence` to `1` (avoids returning unspecified `0`).

## [v0.8.0] - 2025-12-16

### Changed

- **moqt:** Improved message encoding/decoding performance by switching to direct buffer allocation.

## [v0.7.0] - 2025-12-16

### Changed

- **Protocol:** Message length encoding changed from uint16 big-endian to QUIC variable-length integer (varint), aligning with the QUIC spec and reducing overhead for small messages. **Breaking change** — all endpoints must be updated simultaneously.

### Removed

- **moqt:** Removed `bitrate` package (`ShiftDetector`, `EWMAShiftDetector`). The feature depended on non-public forked quic-go APIs, blocking library consumers. Preserved in the `feature/ewma-bitrate-notification` branch.
- **moqt:** Removed Go module `replace` directives for forked dependencies; now using upstream `quic-go` v0.57.1 and `webtransport-go`.

## [v0.6.2] - 2025-12-10

### Changed

- **moqt:** `sendSubscribeStream.UpdateSubscribe()` changed from public to private (`updateSubscribe()`). Use `TrackReader.Update()` as the public API for updating subscription configurations.

## [v0.6.1] - 2025-12-09

### Added

- README translations: Chinese (Simplified), Korean, Russian, German, Japanese.
- Language selection links in all README files.

### Changed

- Repository owner renamed from `OkutaniDaichi0106` to `okdaichi`.
- `Session.SessionUpdated()` renamed to `Session.Updated()`.
- `Session.Terminate()` renamed to `Session.CloseWithError()`.

## [v0.6.0] - 2025-12-05

### Added

- **moqt:** `bitrate` package with `ShiftDetector` interface and `EWMAShiftDetector` for EWMA-based bitrate shift detection.

### Fixed

- **moqt:** `AnnouncementWriter` now calls end functions asynchronously to avoid deadlock.

## [v0.5.0] - 2025-11-27

### Changed

- **Broadcast example:** Switched from LiveKit to UDP as media source.
- **moqt:** `Mux` returns `ErrNoSubscribers` when no subscribers are found, instead of sending GOAWAY.

## [v0.4.3] - 2025-11-26

### Changed

- **moqt:** Distinguish between temporary and permanent errors.

## [v0.4.2] - 2025-11-25

### Fixed

- **moqt:** Fixed duplicate panic in announcement handling.

## [v0.4.1] - 2025-11-24

### Fixed

- **moqt:** `TrackWriter.Close()` now handles stream closure errors correctly.
- **moqt:** Added nil check in `GroupWriter` to prevent panic on uninitialized frame field.

## [v0.4.0] - 2025-11-24

### Added

- **moqt:** New `TrackWriter`, `GroupWriter`, `FrameWriter` track writing API.
- **moqt:** `TrackWriter.Spawn()` for concurrent frame writing.
- **moqt:** `TrackWriter.Write()` for direct frame writing.

### Changed

- **moqt:** `TrackPublisher` replaced by the new `TrackWriter` API.
- **moqt:** `SendSubscribeStream` now returns `*TrackWriter`.

### Removed

- **moqt:** Old `TrackPublisher` API.

## [v0.3.0] - 2025-11-21

### Added

- **moqt:** Native QUIC support with examples in `examples/native_quic`.
- `quic` package wrapping QUIC functionality for the core library and examples.
- README translations: Russian, German, Japanese.

### Changed

- **Dependencies:** Separated QUIC and WebTransport dependencies for flexible usage.

## [v0.2.0] - 2025-11-15

### Added

- **moqt:** WebTransport support via `webtransport` package.
- Interoperability test suite in `cmd/interop`.
- TypeScript client in `moq-web`.

### Changed

- Improved session management and error handling.

## [v0.1.0] - 2025-11-01

### Added

- Initial implementation of MOQ Lite protocol.
- Core `moqt` package with session, track, group, and frame handling.
- Basic examples: broadcast, echo, relay.
- Mage build system integration.
