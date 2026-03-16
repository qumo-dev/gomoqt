# gomoqt

<div align="center">
<sup align="center"><a href="README.ja.md">日本語</a> | <a href="README.de.md">Deutsch</a> | <a href="README.ru.md">Русский</a> | <a href="README.ko.md">한국어</a> | <a href="README.zh-cn.md">简体中文</a></sup>
</div>

A Go implementation of Media over QUIC Transport (MOQT), specifically implementing the MOQ Lite specification for efficient media streaming over QUIC.

[![Go](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml)
[![Lint](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml)
[![moq-web CI](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/OkutaniDaichi0106/gomoqt.svg)](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
[![codecov](https://codecov.io/gh/OkutaniDaichi0106/gomoqt/branch/main/graph/badge.svg?token=4LZCD3FEU3)](https://codecov.io/gh/OkutaniDaichi0106/gomoqt)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/okdaichi/gomoqt)

## Table of Contents
- [Overview](#overview)
- [Quick Start](#quick-start)
- [Features](#features)
- [Components](#components)
- [Examples](#examples)
- [Documentation](#documentation)
- [Specification Compliance](#specification-compliance)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)
- [Acknowledgments](#acknowledgments)

## Overview
This implementation follows the [MOQ Lite specification](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html), providing a foundation for building real-time media streaming applications using QUIC transport.

## Quick Start
```bash
# Install Mage (Go 1.25+)
go install github.com/magefile/mage@latest

# Run the interop test in Docker (TypeScript client + server)
mage interop:ts
# Or run the Go client instead
mage interop:go
```
## Features
- **MOQ Lite Protocol** — Lightweight version of MoQ specification
  - **Low-latency playback** — Minimizes latency from data discovery, transmission/reception to playback
  - **Uninterrupted playback** — Resilient design against network fluctuations through independent data transmission/reception
  - **Network environment optimization** — Enables behavior optimization according to network conditions
  - **Track management** — Publisher/Subscriber model for track data transmission/reception
  - **Efficient multiplexed delivery** — Efficient multiplexing through track announcements and subscriptions
  - **Web support** — Browser support using WebTransport
  - **QUIC native support** — Native QUIC support via `quic` wrappers
- **Flexible dependency design** — Separates dependencies like QUIC and WebTransport, allowing use of only necessary components
- **Examples & Interop** — Sample applications and interop suite in `examples/` and `cmd/interop` (broadcast, echo, relay, native_quic, interop server/client)

### See also
- [moqt/](moqt/) — core package (frames, session, track muxing)
- [msf/](msf/) — MSF catalog, delta, timeline, and catalog-track helper package
- [quic/](quic/) — QUIC wrapper and `examples/native_quic`
- [webtransport/](webtransport/), [webtransport/webtransportgo/](webtransport/webtransportgo/), [moq-web/](moq-web/) — WebTransport and client-side code
- [examples/](examples/) — sample apps (broadcast, echo, native_quic, relay)

## Components
- `moqt` — Core Go package for Media over QUIC (MOQ) protocol.
- `msf` — MOQT Streaming Format catalog, delta, and timeline modeling package.
- `moq-web` — TypeScript implementation for the web client side.
- `quic` — QUIC wrapper utilities used by the core library and examples.
- `webtransport` — WebTransport server wrappers (plus `webtransportgo`).
- `cmd/interop` — Interoperability server and clients (Go/TypeScript).
- `examples` — Demonstration apps (broadcast, echo, native_quic, relay).

## Examples
The [examples](examples) directory includes sample applications demonstrating how to use gomoqt:
- **Interop Server and Client** (`cmd/interop/`): Interoperability testing between different MOQ implementations
- **Broadcast Example** (`examples/broadcast/`): Broadcasting functionality demonstration
- **Echo Example** (`examples/echo/`): Simple echo server and client implementation
- **Native QUIC** (`examples/native_quic/`): Direct QUIC connection examples
- **Relay** (`examples/relay/`): Relay functionality for media streaming

## Documentation
- [GoDoc](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
- [MOQ Lite Specification](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [MSF package README](msf/README.md)
- [Implementation Status](moqt/README.md) — Detailed implementation progress

## Specification Compliance
This implementation targets the MOQ Lite specification, which provides a simplified approach to Media over QUIC Transport. The current implementation status can be found in the [moqt package README](moqt/README.md), which includes detailed tracking of implemented features according to the specification sections.

## Development
### Prerequisites
- Go 1.25.0 or later
- [Mage](https://magefile.org/) build tool (install with `go install github.com/magefile/mage@latest`)

### Development Commands
```bash
# Format code
mage fmt

# Run linter (requires golangci-lint: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
mage lint

# Run quality checks (fmt and lint)
mage check

# Run all tests
mage test:all

# Run tests with coverage
mage test:coverage
```

#### Build & Clean
```bash
# Build the code
mage build

# Clean up generated files
mage clean

# Show available commands
mage help
```

## Contributing
We welcome contributions! Here's how you can help:
1. Fork the repository.
2. Create a feature branch (`git checkout -b feature/amazing-feature`).
3. Make your changes.
4. Verify code quality:
   ```bash
   mage fmt
   mage lint
   mage test
   ```
5. Commit your changes (`git commit -m 'Add amazing feature'`).
6. Push your branch (`git push origin feature/amazing-feature`).
7. Open a Pull Request.

## License
This project is licensed under the MIT License; see [LICENSE](LICENSE) for details.

## Acknowledgments
- [quic-go](https://github.com/quic-go/quic-go) — QUIC implementation in Go
- [webtransport-go](https://github.com/okdaichi/webtransport-go) — WebTransport implementation in Go
- [MOQ Lite Specification](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) — The specification this implementation follows
