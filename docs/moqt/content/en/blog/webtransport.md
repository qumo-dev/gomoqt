---
title: "WebTransport"
date: 3025-10-01
authors:
  - name: okdaichi
    link: https://github.com/okdaichi
    image: https://github.com/okdaichi.png
tags:
  - WebTransport
  - Transport
  - Protocol
  - Web
---

WebTransport is a protocol for secure, efficient, and bidirectional communication over the web using QUIC. It builds on QUIC APIs, allowing web browsers and applications to benefit from QUIC features and the Media over QUIC (MoQ) architecture.

<!--more-->

## Features

1. [[QUIC] Multiplexed Streams](../quic/#1-multiplexed-streams)
2. [[QUIC] Roaming & NAT Traversal](../quic/#2-roaming--nat-traversal)
3. [[QUIC] Load Balancing](../quic/#3-load-balancing)
4. [[QUIC] Security](../quic/#4-security)
5. [[QUIC] Congestion Control](../quic/#5-congestion-control)
6. [[QUIC] Streams and Datagrams](../quic/#6-streams-and-datagrams)
7. [Browser Support](#7-browser-support)

### 7. Browser Support
WebTransport is an evolution of WebSocket, operating over HTTP/3. It is supported by major browsers:
- Supported in Chrome, Edge, and Firefox; Safari support is in progress.


## Go Implementations

The following Go libraries provide experimental or partial support for WebTransport:

### quic-go/webtransport-go
{{<github-readme-stats user="quic-go" repo="webtransport-go" >}}
WebTransport implementation based on quic-go. Actively maintained by the quic-go project.

### adriancable/webtransport-go
{{<github-readme-stats user="adriancable" repo="webtransport-go" >}}
Lightweight but fully-capable WebTransport server for Go.

> Note: WebTransport support in Go is still experimental and evolving. Production use is not yet recommended.