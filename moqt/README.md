# `moqt` package

## Overview

Package moqt implements the core MOQ Lite protocol handling for Media over QUIC Transport. It provides the fundamental primitives (sessions, tracks, frames, and messages) for building real-time media streaming applications over QUIC.

### Specification

This implementation is based on [Media over QUIC - Lite Draft 01](https://datatracker.ietf.org/doc/html/draft-ietf-moq-lite-01). For detailed specification compliance status and implementation differences, see [SPECIFICATION.md](../SPECIFICATION.md) in the root directory.

## Installation

```go
import "github.com/okdaichi/gomoqt/moqt"
```

## Usage

For detailed usage examples and API documentation, see:
- [Documentation](https://okdaichi.github.io/gomoqt/) - Complete API reference and guides
- [Examples](../examples/) - Working examples (broadcast, relay, echo)

## Features & Implementation Status

The following table lists features and tracks implementation progress against
the MOQ Lite specification sections:

| Section                                    | Implemented        | Tested             |
| ------------------------------------------ | ------------------ | ------------------ |
| **1. Data Model**                          |                    |                    |
| 1.1. Frame                                 | :white_check_mark: | :white_check_mark: |
| 1.2. Group                                 | :white_check_mark: | :white_check_mark: |
| 1.3. Track                                 | :white_check_mark: | :white_check_mark: |
| 1.3.1. Track Naming                        | :white_check_mark: | :white_check_mark: |
| **2. Session Management**                  |                    |                    |
| 2.1. Session Establishment                 | :white_check_mark: | :white_check_mark: |
| 2.1.1. WebTransport                        | :white_check_mark: | :white_check_mark: |
| 2.1.2. QUIC                                | :white_check_mark: | :white_check_mark: |
| 2.1.3. Connection URL                      | :white_check_mark: | :white_check_mark: |
| 2.2. Extension Negotiation                 | :x:                | :x:                |
| 2.3. Bitrate Monitoring                    |                    |                    |
| 2.3.1. Bitrate Change Detection            | :white_check_mark: | :white_check_mark: |
| 2.3.2. Bitrate Update Reception            | :white_check_mark: | :white_check_mark: |
| 2.4. Session Termination                   | :white_check_mark: | :white_check_mark: |
| 2.5. Session Migration                     | :construction:     | :x:                |
| **3. Track Publishing**                    |                    |                    |
| 3.1. Publication Announcement              | :white_check_mark: | :white_check_mark: |
| 3.2. Subscription Routing                  | :white_check_mark: | :white_check_mark: |
| 3.3. Track Serving                         | :white_check_mark: | :white_check_mark: |
| 3.4. Graceful Publisher Relay Switchover   | :x:                | :x:                |
| **4. Track Subscription**                  |                    |                    |
| 4.1. Broadcast Discovery                   | :white_check_mark: | :white_check_mark: |
| 4.2. Track Subscription                    | :white_check_mark: | :white_check_mark: |
| 4.3. Graceful Subscriber Relay Switchover  | :x:                | :x:                |


## References

- [MOQ Lite Specification](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [QUIC Transport Protocol](https://tools.ietf.org/html/rfc9000)
- [WebTransport Protocol](https://tools.ietf.org/html/draft-ietf-webtrans-http3/)
- [CanIUse WebTransport](https://caniuse.com/webtransport)
