---
title: "QUIC"
date: 3025-10-01
authors:
  - name: okdaichi
    link: https://github.com/okdaichi
    image: https://github.com/okdaichi.png
tags:
  - QUIC
  - Transport
  - Protocol
---

QUIC is a transport protocol for secure, efficient, and reliable communication over the internet. It supports multiplexed streams, strong security with mandatory TLS, and flexible connection management. QUIC is the foundation for protocols like Media over QUIC (MoQ) and WebTransport.

<!--more-->

## Features

1. [Multiplexed Streams](#1-multiplexed-streams)
2. [Roaming & NAT Traversal](#2-roaming--nat-traversal)
3. [Load Balancing](#3-load-balancing)
4. [Security](#4-security)
5. [Congestion Control](#5-congestion-control)
6. [User Space](#6-user-space)

### 1. Multiplexed Streams
Multiple data streams can be sent and received independently over a single connection. This enables efficient handling of video, audio, control signals, and more—all at the same time.
- No head-of-line blocking
- No need for connection pooling

### 2. Roaming & NAT Traversal
Connections remain stable even if IP or port changes, and using UDP/443 makes it easier to traverse enterprise networks.
- Connection continuity via Connection ID
- Strong support for NAT rebinding and roaming

> **Note**: QUIC is designed to work well in mobile and enterprise environments, supporting connection migration and NAT rebinding for robust connectivity.

### 3. Load Balancing
Flexible load balancing and global deployment are possible with Connection IDs, Anycast, and Preferred Address.
- Supports sticky sessions
- Enables optimal global routing

### 4. Security
TLS is mandatory, and packet header encryption protects against eavesdropping and tampering.
- Connection IDs are recommended to be encrypted
- Robust against middlebox interference

> **Note**: QUIC uses UDP/443 by default, which helps with firewall compatibility and easier traversal of restrictive networks.


### 5. Congestion Control
Applications can implement optimal congestion control (e.g., BBR) for low-latency, high-efficiency delivery.
- No kernel dependencies
- Easy to experiment with new algorithms

> **Developer Highlight**: QUIC is implemented in user space, allowing developers to experiment with custom congestion control and protocol features without kernel changes.

### 6. User Space
QUIC operates in user space and is not implemented in the kernel.

Developers can select or implement congestion control algorithms that best suit their application's requirements, including optimizations for specific scenarios such as live video. User space implementation also enables rapid experimentation and protocol updates without relying on OS or kernel changes, providing greater flexibility. This flexibility is a key reason why QUIC is attractive for modern, latency-sensitive applications.

TCP congestion control, on the other hand, is implemented natively in the kernel of each operating system. While it is technically possible to use a custom TCP stack, this is not practical for most users. Default algorithms are typically loss-based and can result in poor performance for latency-sensitive applications.

However, this also brings drawbacks. Because QUIC cannot leverage kernel optimizations, it may not perform as well as TCP in certain scenarios, and user space implementations can introduce some overhead and inefficiencies.

## Go Implementations

The following Go libraries provide experimental or partial support for QUIC:

### quic-go/quic-go
{{<github-readme-stats user="quic-go" repo="quic-go" >}}
QUIC implementation in pure Go. Actively maintained and widely used.

### golang/net/quic
{{<github-readme-stats user="golang" repo="net" >}}
QUIC support in the Go standard library. This is in progress.

## Native QUIC

## WebTransport