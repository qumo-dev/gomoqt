---
title: MoQ
weight: 5
---

MoQ (Media over QUIC) is a live media delivery protocol based on QUIC. It is designed for low latency, scalability, and relay/cache scenarios, leveraging QUIC/WebTransport's parallel streams and priority control.

## Features

1. [**QUIC**:](#1-quic)
    - [**UDP-based**](#11-udp-based)
    - [**Congestion Control**](#12-congestion-control)
    - [**Encryption**](#13-encryption)
    - [**Multiplexed Streams**](#14-multiplexed-streams)
2. [**Publish / Subscribe**:](#2-publish--subscribe)
3. [**Announce / Discover**:](#3-announce--discover)
4. [**General-purpose Data Model**:](#4-general-purpose-data-model)
5. [**Scalability**:](#5-scalability)
6. [**Web Support**:](#6-web-support)
7. [**Multi-Use**:](#7-multi-use)

### 1. QUIC

#### 1.1. UDP-based
QUIC is a UDP-based protocol. This enables low-latency, connection-oriented communication and efficient handling of real-time media traffic, making MoQ suitable for live streaming and interactive applications. Because QUIC is not TCP, it avoids the Head-of-Line (HoL) blocking issues inherent to TCP. By using independent streams over UDP, QUIC further improves performance for real-time media delivery.

#### 1.2. Congestion Control
QUIC incorporates advanced congestion control mechanisms to optimize network performance and ensure fair bandwidth allocation among multiple streams. By dynamically adjusting the sending rate based on network conditions, QUIC minimizes latency and packet loss, providing a smoother experience for real-time media applications.

#### 1.3. Encryption
QUIC provides built-in encryption for all media streams, ensuring secure transmission by default. QUIC itself requires TLS for all connections, so every session is encrypted from the start. When used with WebTransport over HTTP/3, MoQ benefits from modern security standards and privacy protections for both native and web-based clients.

#### 1.4. Multiplexed Streams
A Stream in QUIC is a communication unit that maintains the order and integrity of data, much like a single TCP connection. QUIC supports multiplexed streams. Each stream ensures that data is delivered in sequence, providing reliable transport for media or other content. By controlling each stream independently, QUIC avoids global Head-of-Line (HoL) blocking that would otherwise occur if unnecessary data integrity enforcement affected the entire connection.

Multiplexed streams allow multiple media tracks or data flows to be transmitted in parallel within a single connection. This enables efficient, prioritized delivery of diverse media content.

### 2. Publish / Subscribe

MoQ adopts a publish/subscribe model, where media sources (publishers) send data to the network, and receivers (subscribers) request and receive only the tracks they are interested in. This model enables efficient, scalable distribution and selective consumption of media streams, reducing unnecessary data transfer and allowing dynamic adaptation to network and user needs.

### 3. Announce / Discover

MoQ provides mechanisms for announcing the availability of media tracks and discovering them on the network. Discovery functionality is defined and integrated at the protocol level, allowing publishers to inform subscribers about new or updated tracks, and enabling subscribers to dynamically discover and select tracks of interest. This built-in approach enables flexible and real-time media session management, supporting scenarios where media sources and content change frequently.

### 4. General-purpose Data Model

MoQ defines a flexible and general-purpose media data model, designed to support a wide variety of applications and evolving requirements. The model consists of:
- **Broadcast**: A namespace containing multiple tracks published by a broadcaster, with availability announced dynamically.
- **Track**: A semantically meaningful media sequence (e.g., video or audio), serving as the unit for subscription and priority control.
- **Group**: A temporal collection of frames (such as a GOP), each with a sequence number, delivered efficiently via QUIC streams.
- **Frame**: The smallest unit of media data within a group, represented as a byte payload.

This structure allows for both dependent and independent delivery of media data, enabling reliable transmission when needed and efficient, scalable distribution for diverse use cases. The data model's adaptability is a key factor in MoQ's suitability for live streaming, interactive communication, gaming, and more.

### 5. Scalability

MoQ is designed for scalable media delivery, supporting relay and cache nodes for efficient forwarding and storage. Its Server/Client model centralizes management and improves compatibility with web and NAT/firewall environments. Distributed relay and cache nodes help avoid single points of failure and balance server load, ensuring reliable performance for large audiences and dynamic sources.

### 6. Web Support

Thanks to WebTransport over HTTP/3, MoQ enables secure and efficient media delivery directly in browsers. This allows real-time streaming and interactive applications in web environments without the need for plugins or custom protocols.

### 7. Multi-Use

MoQ is designed as a general-purpose media protocol, addressing limitations found in earlier, more specialized solutions. Its flexible data model and protocol architecture allow it to support a wide range of applications—from live streaming and interactive communication to gaming and multi-layer video—making it adaptable to diverse and evolving media needs.

## Use Cases
- Live streaming
- Interactive conferencing
- Gaming/Interactive
- Multi-layer video

## Specifications

There are two different MoQ specifications: **[MoQ Transport(moqt)](https://datatracker.ietf.org/doc/draft-ietf-moq-transport)** and **[MoQ Lite(moql)](https://datatracker.ietf.org/doc/draft-lcurley-moq-lite/)**.

**MoQ Lite** is a lightweight MoQ protocol designed simple to build something easier that you can use now, even if it’s not a standard yet.
`gomoqt` is an implementation of MoQ Lite.
MoQ Transport is the standardizing MoQ protocol under the IETF. It is more complex than MoQ Lite.
`gomoqt` does not yet support MoQ Transport.

## References
- MoQ WG: https://datatracker.ietf.org/wg/moq/about/
- MoQ Lite: https://datatracker.ietf.org/doc/draft-lcurley-moq-lite/
- MoQ Transport: https://datatracker.ietf.org/doc/draft-ietf-moq-transport/
- WebTransport over HTTP/3: https://datatracker.ietf.org/doc/draft-ietf-webtrans-http3/
- QUIC: https://www.rfc-editor.org/rfc/rfc9000.html
- HTTP/3: https://www.rfc-editor.org/rfc/rfc9114.html
