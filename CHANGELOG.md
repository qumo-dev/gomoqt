# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

### Added

- **transport:** introduced a shared transport abstraction package (`transport/*`) and migrated core connection/listener/config/error interfaces into it.

### Changed

- **moqt core:** migrated client/session/server and internal QUIC/WebTransport wrappers to the shared `transport` package APIs.
- **moqt/upgrader:** added support for customizable WebTransport application protocols via `Upgrader.ApplicationProtocols`.
- **interop:** updated Dockerized interop flow and Go interop client/server URL handling for more robust address/scheme behavior.
- **interop/Dockerfile:** aligned containerized interop environment with updated Go toolchain/runtime settings.
- **examples:** updated broadcast and echo server examples to the current `Upgrader` API.
- **Dependencies:** switched WebTransport dependency from `github.com/quic-go/webtransport-go` to `github.com/okdaichi/webtransport-go v0.10.1-okdaichi.1`.
- **webtransport/webtransportgo:** updated wrapper imports to use the forked WebTransport module path.
- **webtransport/webtransportgo:** adapted server wrapper initialization for fork API differences (removed dependency on `ConfigureHTTP3Server`).
- **Documentation:** updated README files (all supported languages) and MoQT docs links to reference `github.com/okdaichi/webtransport-go`.

### Fixed

- **moqt/server:** wire server `connContext` into the default internal WebTransport server (`http3.Server.ConnContext`) so request contexts include MOQ server metadata during WebTransport upgrades.
- **moqt/server:** fix `connContext` variable shadowing so custom `Server.ConnContext` return values are correctly propagated.

### Removed

- **moqt:** removed unused router/session-stream related code paths as part of the transport-layer refactor.
- **moq-web:** removed `SessionStream` and related legacy session-stream wiring to simplify session management.
- **examples:** cleaned up outdated relay/native_quic example code paths.

### Tests

- **moqt:** migrated test suite to the new session/server transport APIs after transport-layer refactor.
- **webtransport/webtransportgo:** updated server init tests to validate stable wrapper initialization behavior across the forked implementation.
- **moqt/server:** added regression tests to verify default WebTransport `ConnContext` wiring and `connContext` behavior (custom context propagation and nil-context panic guard).

## [v0.11.0] - 2026-03-12

### Added

- **moq-web/msf:** adopted `zod` for JSON boundary validation in catalog, catalog delta, and timeline parsing.

### Changed

- **moq-web/msf:** tightened parse-time validation for invalid JSON field types while preserving existing higher-level catalog validation APIs.
- **moq-web:** bumped the published package version to `0.11.0`.

## [v0.10.8] - 2026-03-12

### Changed

- **moq-web:** reorganized MSF code into `src/msf/` directory, introduced generic `Broadcast` class with accompanying tests, and added MSF-aware broadcast implementation.
- **moq-web:** exposed `NotFoundTrackHandler` and added `NotFound` helper for API parity with Go.
- **moq-web:** cleaned up root-level MSF files and updated exports (`mod.ts`, `deno.json`).


## [v0.10.7] - 2026-02-27

### Added

- Non-root `appuser` and workspace ownership in Dockerfile.
- Tests for `buildTSClientCmd` and error handling when `moq-web` is missing.

### Changed

- Wired client-supported versions through `responseWriter` and updated constructors/tests.
- Revamped `session_stream` channel-closing logic to avoid races.
- Dropped misleading `moq-web` fallback in interop command.
- Updated multiple README documents across languages.
- Misc test adjustments and ts client refactor.

### Fixed

- mkcert invocation now fails build on error.
- Deno install owned by non-root.
- Several server and session bugs surfaced by new tests.


## [v0.10.5] - 2026-02-20

### Added

- Allow overriding server address in interop client via `INTEROP_ADDR` environment variable (daichiDeskTop)
- Add magefile for build automation and environment setup (daichiDeskTop)

### Changed

- Simplify `GroupReader`/`GroupWriter` tests by using `Buffer` for encoding (OkutaniDaichi0106)
- Refactor `GroupReader` tests to add EOFError handling and `frames()` async iterator (OkutaniDaichi0106)
- Remove unused logger parameter from `acceptSessionStream` function (daichiDeskTop)
- Simplify verbose comments to focus on spec/behaviour (daichiDeskTop)
- Add regression tests for webtransport-go v0.10.0 compatibility and server shutdown (daichiDeskTop)

### Fixed

- Call `ConfigureHTTP3Server(wtserver.H3)` in `NewServer()` (daichiDeskTop)
  - **webtransport/webtransportgo:** without this, `H3.ConnContext` is `nil` and `Server.Upgrade()` cannot retrieve the QUIC connection from the HTTP request context, returning `"webtransport: missing QUIC connection"` on every WebTransport upgrade attempt.
  - `ConfigureHTTP3Server` performs three necessary steps:
    - sets `H3.AdditionalSettings[settingsEnableWebtransport] = 1` (WebTransport protocol negotiation)
    - sets `H3.EnableDatagrams = true` (HTTP/3-level datagram support)
    - installs `H3.ConnContext` to inject `*quic.Conn` into each HTTP/3 request context so `Upgrade` can retrieve it


## [v0.10.4] - 2026-02-20

### Fixed

- **webtransport/webtransportgo:** initialize HTTP/3 server pointer in `NewServer()` to avoid a nil-pointer panic with `github.com/quic-go/webtransport-go v0.10.0`.

- **moqt:** automatically enable QUIC flags required by WebTransport in `ListenAndServe()` / `ListenAndServeTLS()` (`EnableDatagrams` and `EnableStreamResetPartialDelivery`) so WebTransport connections work by default.

- **moqt:** fix server shutdown hang — ensure listener goroutines complete before clearing the listeners map (prevents a WaitGroup leak in `Close()` / `Shutdown()`).

### Changed

- **moqt:** WebTransport-compatible QUIC configuration flags are now enabled by default; callers no longer need to set them explicitly for typical WebTransport use.


## [v0.10.2] - 2026-02-10

### Fixed

- **moq-web: Added validation for maximum varint size in writeVarint function**
  - Added check for `Infinity` and values exceeding `MAX_VARINT8`
  - Now properly returns `RangeError` for invalid values instead of attempting to write them
  - Ensures consistent error handling for varint encoding edge cases

### Changed

- **moqt: Improved announcement handling and server closure safety**
  - Enhanced nil checks for context in AnnouncementReader, AnnouncementWriter, receiveSubscribeStream, and sendSubscribeStream
  - Improved announcement handling logic
  - Enhanced server closure safety mechanisms

- **moqt: Code refactoring and cleanup**
  - Simplified buffer initialization in message handling
  - Improved logging in moqt package
  - Removed StreamID method from stream wrappers for cleaner API
  - Removed config parameter from acceptSessionStream function

- **Dependencies: Updated QUIC and WebTransport libraries**
  - Updated `github.com/quic-go/quic-go` to v0.59.0
  - Updated `github.com/quic-go/webtransport-go` to v0.10.0


## [v0.10.1] - 2026-01-04

### Fixed

- **moq-web: Fixed QUIC varint encoding/decoding for values exceeding 32 bits**
  - JavaScript bitwise operations are limited to 32 bits, causing incorrect encoding/decoding of 8-byte varints
  - Changed to use division for shifts exceeding 32 bits in `writeVarint()`
  - Fixed `readVarint()` and `parseVarint()` to correctly handle values up to 53 bits (JavaScript Number precision limit)
  - This fix ensures proper operation in browser environments for large sequence numbers and object IDs

### Changed

- **moq-web: Enhanced GroupStream API to accept Uint8Array directly**
  - `GroupWriter.writeFrame()` now accepts `ByteSource | Uint8Array`
  - `GroupReader.readFrame()` now accepts `ByteSink | ByteSinkFunc`
  - Maintains backward compatibility with existing Frame-based code


## [v0.10.0] - 2026-01-04

### Changed

- **moq-web: Frame API redesign with ByteSource/ByteSink pattern**
  - Introduced `ByteSource` and `ByteSink` interfaces for flexible data handling
  - Replaced direct `bytes` property access with `ByteSource.copyTo()` method for safer data access
  - Implemented `ByteSinkFunc` type for functional-style data writing
  - Updated `BytesBuffer` to implement both `ByteSource` and `ByteSink` interfaces
  - Modified `GroupReader.readFrame()` to accept `ByteSink | ByteSinkFunc` for flexible data consumption
  - Improved buffer management with proper bounds checking in `copyTo()` method

### Fixed

- **moq-web: Fixed buffer overflow in Frame.copyTo()**
  - Added `Math.min()` check to prevent out-of-bounds access when internal buffer size doesn't match data length
  - Fixed RangeError in interop tests caused by incorrect Frame usage pattern

- **moq-web: Fixed TypeScript type errors in mock stream implementations**
  - Properly wrapped partial stream methods to ensure they always return Promises
  - Eliminated type mismatches between sync and async return types in MockSendStream and MockReceiveStream

### Tests

- moq-web: Updated all Frame-related tests to use new `ArrayBuffer` constructor and `write()` method pattern
- moq-web: Updated `group_stream_test.ts` to use `copyTo()` method instead of direct `bytes` property access
- moq-web: Updated `group_stream_benchmark.ts` with new Frame creation patterns


## [v0.9.0] - 2025-12-24

### Added

- moqt: `OpenGroupAt(seq GroupSequence)` public API to open a group with an explicit sequence number. When a sequence is specified, the internal next-sequence counter is advanced atomically to at least `seq+1` to prevent collisions with subsequently auto-assigned sequences. (See `moqt/track_writer.go` and `moqt/track_writer_test.go`)
- moq-web: concurrency test for `ReceiveSubscribeStream.writeInfo` ensuring `SUBSCRIBE_OK` is sent only once when `writeInfo` is called concurrently. (See `moq-web/src/subscribe_stream_test.ts`)

### Changed

- moqt: `OpenGroup()` autoincrement behavior adjusted to return sequences starting from `0` (first created group has sequence `0`), and subsequent groups increment from there. Tests updated to reflect the new baseline behavior.
- moqt: clarified `OpenGroup` / `OpenGroupAt` comments to document caller responsibilities and concurrent behavior.
- Use Go builtin `max` where appropriate to improve clarity and express intent.

### Fixed

- moqt: `GroupSequence.Next()` behavior adjusted to wrap from `MaxGroupSequence` to `1` (avoid returning unspecified `0`). Tests updated accordingly.

### Tests

- moqt: Added tests: `TestTrackWriter_OpenGroupAtAdvancesSequence` and `TestTrackWriter_OpenGroupAtConcurrent` to verify explicit sequence assignment advances internal counter and to ensure no duplicate sequences under concurrent usage.
- moq-web: Added `ReceiveSubscribeStream writeInfo is only executed once even with concurrent calls` test to verify `Once`-based deduplication of `SUBSCRIBE_OK`.


## [v0.8.0] - 2025-12-16

### Changed

- **Message encoding/decoding performance improvement**: Replaced sync.Pool-based buffer pooling with direct allocation
  - Benchmark results showed that direct allocation (`make([]byte, 0, cap)`) significantly outperforms pool-based allocation for typical message sizes
  - Small messages (10 bytes): 5.9x faster, 28x less memory with direct allocation
  - Medium messages (80 bytes): 3.4x faster, 3.8x less memory with direct allocation
  - Parallel execution: 13.8x faster with direct allocation
  - Pool overhead (mutex locks, type assertions, pointer operations) exceeds allocation cost for small-to-medium sized messages
  - Modern Go runtime's allocator is highly optimized for small allocations, making pool unnecessary

### Removed

- `bytes_pool.go` and all `pool.Get()`/`pool.Put()` calls (replaced with `make([]byte, 0, cap)`)

## [v0.7.0] - 2025-12-16

### Changed

- **Message Length Encoding**: Changed message length encoding from uint16 big-endian to QUIC variable-length integer (varint)
  - Message length is now encoded using standard QUIC varint format (1, 2, 4, or 8 bytes depending on value)
  - This change aligns the implementation with the QUIC specification and improves efficiency for small messages
  - Messages up to 63 bytes now use only 1 byte for length (previously always 2 bytes)
  - Maximum message size increased from 65,535 bytes to 2^62-1 bytes
  - **Breaking Change**: This is a protocol-breaking change. Old clients and servers cannot communicate with new ones
  - **Migration Guide**: All endpoints must be updated simultaneously to maintain compatibility

### Removed

- **EWMA Bitrate Notification**: Removed experimental EWMA-based bitrate notification feature (v0.6.0)
  - Removed `moqt/bitrate/` package (ewma.go, ewma_test.go, shift_detector.go)
  - Removed `NewShiftDetector` field from `Config`
  - Removed `ConnectionStats()` method from `quic.Connection` interface
  - **Reason**: Feature depended on non-public APIs from forked quic-go, causing instability and preventing library users from using the package due to Go module replace directive limitations
  - **Migration Guide**: This feature has been preserved in the `feature/ewma-bitrate-notification` branch for reference
  - `Session.goAway()` is now a no-op (graceful shutdown is handled by QUIC connection close)
- **Go Module Replace Directives**: Removed replace directives for forked dependencies
  - No longer using `github.com/okdaichi/quic-go` or `github.com/okdaichi/webtransport-go`
  - Now using upstream `github.com/quic-go/quic-go` v0.57.1 and `github.com/quic-go/webtransport-go`
  - **Impact**: Library can now be used as a dependency without type compatibility issues
  - All tests passing with upstream dependencies

### Performance

- **TrackMux Advanced Optimizations**: Further improved performance with lock contention reduction and memory efficiency
  - **Lock Optimization**: Reduced lock hold time in `findTrackHandler` by performing all checks within single RLock
  - **Memory Allocation**: Moved handler struct allocation outside critical section in `registerHandler`
  - **Code Deduplication**: Refactored `serveTrack` to reuse optimized `findTrackHandler`, eliminating duplicate lock acquisition
  - **Read-Write Lock Pattern**: Implemented double-check locking in `getChild` to minimize write lock contention
  - **Worker Pool Enhancement**: Optimized `Announcement.end()` with inline execution for small handler counts and efficient work distribution
  - **Results**: Handler lookup improved to 21-25ns (48-51% from baseline, 12-20% from first optimization)

- **Initial TrackMux Optimizations**: Improved performance of track handler lookups and announcements
  - Reduced lock contention in `findTrackHandler` by simplifying map lookups
  - Pre-allocated maps with initial capacity to reduce allocations during runtime
  - Removed unnecessary defer statements for faster lock/unlock operations
  - Pre-allocated slices in `Announce` function to reduce dynamic allocations
  - **Results**: Handler lookup improved by 42-67% (41ns → 24-31ns), ServeTrack improved by 23% (243ns → 187ns), GC overhead reduced from 55% to 25%

### Fixed

- **Benchmark Test Mocks**: Fixed `BenchmarkTrackMux_ServeAnnouncements` by adding required mock expectations for `Context()` and `Write()` methods

## [v0.6.2] - 2025-12-10

### Changed

- **API Encapsulation**: Changed `sendSubscribeStream.UpdateSubscribe()` from public to private (`updateSubscribe()`) to improve API boundaries
  - `TrackReader.Update()` remains the only public API for updating subscription configurations
  - Prevents unintended direct access to internal implementation methods while maintaining embedding benefits

## [v0.6.1] - 2025-12-09

### Added

- Chinese (Simplified) translation of README (`README.zh-cn.md`)
- Korean translation of README (`README.ko.md`)
- Chinese translation of README (`README.zh.md`)
- Russian translation of README (`README.ru.md`)
- German translation of README (`README.de.md`)
- Japanese translation of README (`README.ja.md`)
- Language selection links in all README files for improved accessibility
- Detailed README files for interop, examples, and moqt package

### Changed

- **Repository ownership**: Changed GitHub username from `OkutaniDaichi0106` to `okdaichi`
- **Session API naming**: Renamed `Session.SessionUpdated()` to `Session.Updated()`
- **Session API naming**: Renamed `Session.Terminate()` to `Session.CloseWithError()` for consistency
- **Documentation**: Updated all documentation to align with current implementation and reflect correct GitHub username
- **Documentation**: Improved README formatting and features section clarity across all languages
- **Dependencies**: Updated module replace directives to use forked quic-go and webtransport-go commits

## [v0.6.0] - 2025-12-05

### Added

- `bitrate` package: Bitrate monitoring functionality with `ShiftDetector` interface and `EWMAShiftDetector` implementation for detecting bitrate shifts using Exponential Weighted Moving Average

### Changed

- Modernize test code: Replace traditional for loops with range loops

### Fixed

- `AnnouncementWriter`: Avoid deadlock by calling end functions asynchronously

## [v0.5.0] - 2025-11-27

### Changed

- **Broadcast example**: Switch from LiveKit to UDP as media source
- **Mux error handling**: Return `ErrNoSubscribers` on failure to find subscribers instead of GOAWAY

## [v0.4.3] - 2025-11-26

### Changed

- **Error handling**: Distinguish temporary and permanent errors

## [v0.4.2] - 2025-11-25

### Fixed

- Fix duplicate panic in announcement handling

## [v0.4.1] - 2025-11-24

### Fixed

- **TrackWriter**: Handle stream closure errors in `TrackWriter.Close()`
- **GroupWriter**: Add nil check for frame field to prevent panic

## [v0.4.0] - 2025-11-24

### Added

- New track writer implementation (`TrackWriter`, `GroupWriter`, `FrameWriter`)
- Concurrent frame writing support via `TrackWriter.Spawn()`
- `TrackWriter.Write()` method for direct frame writing
- Generic parameter type support for `TrackConfig`

### Changed

- **API redesign**: Replace `TrackPublisher` with new `TrackWriter` API
- **Parallel writing**: Simplify parallel group writing with direct track writer operations
- **SendSubscribeStream**: Now returns `*TrackWriter` instead of `TrackPublisher`

### Removed

- Old `TrackPublisher` API

## [v0.3.0] - 2025-11-21

### Added

- **Native QUIC support**: Direct QUIC connection examples in `examples/native_quic`
- `quic` package: Wrapper for QUIC functionality used by core library and examples
- Russian translation of README (`README.ru.md`)
- German translation of README (`README.de.md`)
- Japanese translation of README (`README.ja.md`)

### Changed

- **Dependencies**: Separate QUIC and WebTransport dependencies for flexible usage
- **Examples**: Demonstrate both WebTransport and native QUIC usage

## [v0.2.0] - 2025-11-15

### Added

- **WebTransport support**: Via `webtransport` package
- **Interoperability testing**: Testing suite in `cmd/interop`
- **TypeScript client**: Implementation in `moq-web`

### Changed

- Improve session management and error handling

### Documentation

- Update documentation with WebTransport examples

## [v0.1.0] - 2025-11-01

### Added

- Initial implementation of MOQ Lite protocol
- Core `moqt` package with session, track, group, and frame handling
- Basic examples: broadcast, echo, relay
- Mage build system integration
- Comprehensive test coverage
- MIT License

[Unreleased]: https://github.com/okdaichi/gomoqt/compare/v0.11.0...HEAD
[v0.11.0]: https://github.com/okdaichi/gomoqt/compare/v0.10.8...v0.11.0
[v0.10.8]: https://github.com/okdaichi/gomoqt/compare/v0.10.7...v0.10.8
[v0.10.7]: https://github.com/okdaichi/gomoqt/compare/v0.10.6...v0.10.7
[v0.10.6]: https://github.com/okdaichi/gomoqt/compare/v0.10.5...v0.10.6
[v0.10.5]: https://github.com/okdaichi/gomoqt/compare/v0.10.4...v0.10.5
[v0.10.4]: https://github.com/okdaichi/gomoqt/compare/v0.10.3...v0.10.4
[v0.10.3]: https://github.com/okdaichi/gomoqt/compare/v0.10.2...v0.10.3
[v0.9.0]: https://github.com/okdaichi/gomoqt/compare/v0.8.0...v0.9.0
[v0.8.0]: https://github.com/okdaichi/gomoqt/compare/v0.7.0...v0.8.0
[v0.7.0]: https://github.com/okdaichi/gomoqt/compare/v0.6.2...v0.7.0
[v0.6.2]: https://github.com/okdaichi/gomoqt/compare/v0.6.1...v0.6.2
[v0.6.1]: https://github.com/okdaichi/gomoqt/compare/v0.6.0...v0.6.1
[v0.6.0]: https://github.com/okdaichi/gomoqt/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/okdaichi/gomoqt/compare/v0.4.3...v0.5.0
[v0.4.3]: https://github.com/okdaichi/gomoqt/compare/v0.4.2...v0.4.3
[v0.4.2]: https://github.com/okdaichi/gomoqt/compare/v0.4.1...v0.4.2
[v0.4.1]: https://github.com/okdaichi/gomoqt/compare/v0.4.0...v0.4.1
[v0.4.0]: https://github.com/okdaichi/gomoqt/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/okdaichi/gomoqt/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/okdaichi/gomoqt/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/okdaichi/gomoqt/releases/tag/v0.1.0
