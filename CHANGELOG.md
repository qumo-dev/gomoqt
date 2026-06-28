# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **moqt:** New benchmark coverage for previously-unbenched routing/path primitives: `BenchmarkPrefixSegments` (the `prefixSegments` sibling of `pathSegments` — rewritten in #253 but, unlike `pathSegments`, had no dedicated benchmark), `BenchmarkBroadcastPath_HasPrefix/GetSuffix/Extension/Equal`, and `BenchmarkVarintLen/StringLen/BytesLen/StringArrayLen` (the message pre-sizing helpers called by every Encode). All report 0 allocs/op except `prefixSegments`, which allocates one segment slice scaling with depth.
- **moqt:** New benchmark coverage for the SUBSCRIBE control plane and connection lifecycle: `BenchmarkConnManager_AddRemove`/`_Snapshot` (the conn-manager snapshot that scales with connection count on every `Server.Close`/`Shutdown`), and `BenchmarkReceiveSubscribeStream_WriteInfo`/`WriteDrop`, `BenchmarkSendSubscribeStream_UpdateSubscribe`, and `BenchmarkReadSubscribeResponse` (the subscribe-stream glue around message Encode/Decode). These surface per-call allocations the message-level benchmarks do not — the `[]byte{byte(msgType)}` type-prefix writes and the `make([]byte, 1)` response head.
- **moqt:** New system-level fan-out benchmark PAIR isolating the two scaling axes of relay fan-out (both real-QUIC integration benches: loopback, `-short`-gated, measured in the `benchmark-integration` job). `BenchmarkFanOut_ViewerConnections` (1 track → N INDEPENDENT viewer QUIC connections — the broadcast/live model; `subs-{1,4,16,64,256}`) plus `BenchmarkFanOut_ViewerConnectionsLatency` (its latency lens: per-viewer one-way publish→deliver p50/p95/p99/max + fairness spread; gated publisher + monotonic `time.Since` stamp), and `BenchmarkFanOut_TracksPerConnection` (1 connection → N DISTINCT tracks — the contrast axis that isolates per-track from per-connection cost; `tracks-{1,4,16}`, publishers gated so all subscribes complete before blasting — ungated, already-blasting publishers starved later subscribe handshakes). They fill the gap no existing harness covers (steady-state, long-lived fan-out; `BenchmarkBroadcastServer_HighLoad` reconnects every client every iteration and measures connection churn, not fan-out). Comparing ViewerConnections vs TracksPerConnection at the same N answers whether the fan-out ceiling is per-connection or per-track. Both throughput benchmarks also emit advisory cross-reader fairness metrics (per-reader min/median/mean/p95/p99/max throughput, Jain's fairness index, starved-reader count) to begin characterizing fairness within a shared congestion-control domain — the data a future relay aggregate-vs-shard decision would need.
- **moq-web:** `message_benchmark.ts`, a `Deno.bench` suite for the message codec hot path — varint encode/decode (`writeVarint`/`readVarint`/`parseVarint` across all four widths), length-prefixed string/string-array encode, and `GroupMessage`/`SubscribeMessage` `encode`/`decode` round trips. Uses minimal in-memory `Reader`/`Writer` to isolate codec CPU from stream and async overhead. Establishes a baseline before optimizing the TS serialization layer.
- **moq-web:** `deno task bench` and a `bench` config block (`include: src/**/*_benchmark.ts`) so the benchmark suite is discoverable and runnable as a unit.

### Changed

- **moqt:** `BenchmarkTrackMux_MemoryUsage` no longer emits a custom `allocs/op` metric computed from a `runtime.MemStats` TotalAlloc diff — it collided with `-benchmem`'s real `allocs/op` under the same label and was dominated by `fmt.Sprintf` allocations inside the timed loop. Paths are now pre-generated outside the loop; `b.ReportAllocs` provides the single, accurate allocs/op and B/op.
- **moqt:** `BenchmarkSession_ContextCancellation` removed the `time.Sleep(time.Millisecond)` inside its `b.N` loop — it dominated ns/op (wall-clock jitter, not the close path). `Session.CloseWithError` already drains stream-handling goroutines via `wg.Wait()`, so the sleep was redundant.
- **moqt/internal/message:** `BenchmarkWriteMessageLength` reuses one destination buffer across iterations instead of `make([]byte, 0, n)` per iteration, so the reported allocs/op reflects `WriteMessageLength`'s true cost (0) rather than the harness slice. Uses a constant-capacity destination (matching `BenchmarkWriteVarint`) to avoid an exact-cap escape-analysis artifact on the 1-byte case.
- **moq-web:** Repaired the stale `stream_memory_benchmark.ts` so it type-checks under the new `bench` task — the `ReceiveStream`/`SendStream` constructors no longer accept `streamId`. No behavior change.

### Removed

- **moqt:** Removed the empty `group_manager.go` — a dead leftover from the refactor that renamed `groupManager` to `groupReaderManager`/`groupWriterManager` (eb00586). It declared nothing; the types live in `group_reader.go`/`group_writer.go`.

## [v0.16.0] - 2026-06-26

### Added

- **moq-web:** `TrackWriter.openGroup`/`openGroupAt` and `StreamConn.openStream`/`openUniStream` now accept an optional `AbortSignal` to cancel or time out stream creation, mirroring Go's `OpenGroup(ctx)` → `openUniStreamFunc(ctx)`. The signal is composed internally with `Promise.race` (golikejs `watchSignal`) against the track context; an aborted open never leaves an orphaned stream. Also fixes a leak where a failed group-header write left the allocated stream half-open (Go always `CancelWrite`s).
- **moqt:** `BenchmarkEgress_Saturating`, a saturating real-QUIC egress benchmark that drives `GroupWriter.WriteFrame` end-to-end (encode → QUIC stream → UDP). Run in the `benchmark-integration` job, not on PRs.

### Changed

- **moqt:** `Frame.decode()` now uses exponential capacity growth (`newCap := max(required, 2*cap(slice))`) instead of exactly-sized allocations, significantly reducing GC pressure and maintaining proper struct buffer invariants.
- **moqt/internal/message:** The `*_Encode` benchmarks now write to `io.Discard` instead of a `bytes.Buffer`. The Buffer's internal growth on `Write` was counted by `-benchmem`, inflating the reported Encode cost to ~3 allocs/op when Encode's own allocation is only 1 scratch buffer (~8–96 B by message size). No production behavior change; this corrects the measurement. (A production memprofile of the broadcast path confirms `message.Encode` is not an allocation hot spot — `(*Frame).decode`, QUIC stream setup, and `slog` dominate.)
- **moqt/internal/message:** `ReadMessageLength` now fast-paths `io.ByteReader` (bytes.Reader, bufio.Reader, QUIC streams), eliminating a per-call heap allocation (1 → 0) and ~65% faster varint-length decoding. Behavior is unchanged; non-ByteReader callers use the previous allocating path.
- **Dependencies:** Updated `quic-go` to v0.60.0.
- **moqt:** **Breaking:** `TrackWriter.OpenGroup` and `OpenGroupAt` now take a `context.Context` as their first argument. Update call sites: `tw.OpenGroup()` → `tw.OpenGroup(ctx)`.

### Deprecated

- **transport:** `StreamConn.OpenStream` and `OpenUniStream`; use `OpenStreamSync` / `OpenUniStreamSync`, which backpressure on the peer's stream-limit flow control instead of returning a stream-limit error. Slated for removal in a future release.

### Removed

- **moqt:** Removed the unused `Parameters` type and its `ParametersLen` / `WriteParameters` helpers from the internal `message` package; they had no production callers.

### Fixed

- **moqt:** Outgoing streams now backpressure on the peer's stream limit instead of aborting. Group streams open via `OpenUniStreamSync` (`OpenGroup`/`OpenGroupAt`) and control streams (`Subscribe`, `Fetch`, `AcceptAnnounce`, `Probe`, `Server.goAway`) via `OpenStreamSync` — both are now declared on `StreamConn`. Previously a stream-limit error could abort the publisher or stall the connection.
- **Security (moqt/internal/message, moqt/frame.go):** Fixed an Out-of-Memory (OOM) Denial-of-Service vulnerability. Message and frame `Decode` allocated byte slices directly from an untrusted QUIC varint length prefix (`make([]byte, size)`), so a peer could trigger a multi-GB allocation with a single packet and crash the process. Decoding now rejects lengths exceeding `MaxMessageSize` (50 MB) with `ErrMessageTooLarge` before allocating.
- **moqt:** `Server` now wires a `WebTransportHandler` (built from the Server's `Handler` / `TrackMux` / `Config`) as the default WebTransport handler. Previously `Server.init()` passed a nil HTTP handler, so every WebTransport request fell through to `http.DefaultServeMux` (no MOQ route) and 404'd — the Server's default WebTransport path was non-functional, and `BenchmarkBroadcastServer_HighLoad` silently reported 0 frames/op.
- **moqt:** `Server.Close()` no longer hangs when connections are active. Previously the "terminate sessions" loop had an empty body (active connections were never closed) and sessions were removed from the connection manager only on an explicit, successful `CloseWithError` — so peer-/force-closed sessions leaked and `Server.Close()`/`Shutdown()` blocked forever on `<-connManager.Done()`. `Server.Close()` now closes active connections, the serving paths (`WebTransportHandler.ServeHTTP`, `handleNativeQUIC`) clean up the session when the Handler returns, and `Session.CloseWithError` always removes the connection from the manager.

## [v0.15.0] - 2026-04-26

### Added

- **moqt:** `Session.Stats()` returns `SessionStats` with estimated bitrate, transport RTT, bytes sent, and bytes received.
- **moqt/moq-web:** Added comprehensive concurrent access tests for probe methods.

### Changed

- **moqt/moq-web:** Refactored `bitrateTracker` to be an active monitor, encapsulating its own monitoring loop and sampling logic.
- **moqt/moq-web:** Moved communication channels (`probeResponse` and `probeTargets`) back to the `Session` class, separating measurement from notification.
- **moqt/moq-web:** Implemented robust "latest-value" semantics (buffer size of 1) for all probe notifications.
- **moq-web:** Upgraded to `@okdaichi/golikejs@0.9.0` and integrated the native `Channel<T>` class for simplified asynchronous iteration.
- **moq-web:** Optimized `probe()` and `probeTargets()` to return native iterators directly.

### Fixed

- **moqt/moq-web:** Fixed a delta detection bug where raw measurements were incorrectly compared against themselves instead of the last sent baseline.
- **moqt:** Resolved a deadlock in tests by correctly linking mock stream contexts to the session lifecycle.
- **moq-web:** Fixed an initialization race condition in TypeScript where initial probe messages were sometimes omitted.
- **moqt:** Corrected visibility of internal components by unexporting `bitrateTracker` and its methods.

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
