---
title: "What is MoQ (to be)"
date: 3025-09-01
authors:
  - name: okdaichi
    link: https://github.com/okdaichi
    image: https://github.com/okdaichi.png
tags:
  - MoQ
  - Protocol
---

For building real-time media applications, the choice of transport protocol is crucial. We already have several options, such as WebRTC, WebSocket, and HTTP Streaming Protocols. MoQ (Media over QUIC) is also one of the candidates. What is MoQ, and why should we consider it?

<!--more-->

## Introduction



### MoQ

MoQ (Media over QUIC) is a protocol designed for real-time media transmission over the internet, leveraging the capabilities of the QUIC transport protocol. QUIC is a modern transport protocol that takes the best aspects of UDP, TCP, HTTPS, and other protocols.

### WebRTC

WebRTC is a widely used for real-time communication that provides a comprehensive solution for peer-to-peer media transmission.
It is so native to peer-to-peer communication that, to scale WebRTC, you need to use a Selective Forwarding Unit (SFU) as a middle server. SFUs resolve routing and forwarding control and avoid the too many connections problem. However, SFU is not a protocol but a server implementation. Therefore, there is no standard way to communicate between clients and SFUs. This leads to interoperability issues and vendor lock-in.

## What is in and What is out?

To deliver real-time media, applications have to be able to communicate information such as media data, control messages, and session management.
Here is what they need to communicate:
- Session Management: Establishing, maintaining, and terminating sessions.
- Forwarding Control: Controlling the flow of media data, including routing, congestion control and quality adaptation.
- Media Configuration: Negotiating codecs, bitrates, and other media parameters. It varies depending on the data type.
- Media Data: The actual audio, video, or other media content.

In peer-to-peer conversations or in a conferencing room where everyone shares the same context, it is enough to communicate this information in its own language all at once. However, if you were to play the telephone game, you would quickly realize that this approach does not scale well. In large-scale scenarios, such as broadcasting or large conferences, it is essential to have a common language that everyone can understand.

### Hop-by-Hop and End-to-End

There are two main scenarios where information needs to be communicated: hop-by-hop and end-to-end.
- Hop-by-Hop: Information that needs to be communicated between each node in the network, such as routing and forwarding control.
- End-to-End: Information that needs to be communicated directly between the sender and receiver, such as media configuration and session management.

Either protocols such as WebRTC or MoQ just define how two endpoints communicate, though the handling of hop-by-hop versus end-to-end differs.
In MoQ, information is divided into hop-by-hop and end-to-end. This allows for a more modular approach, where each node only needs to understand the relevant parts of the protocol. This can lead to better scalability and interoperability.
Apart from that, in WebRTC, all information is communicated end-to-end. This means that each client needs to understand the entire protocol stack, which can be complex and lead to interoperability issues.

### Per-Packet vs Per-Stream

In WebRTC, media data are delivered in individual packets (RTP Packets).
In MoQ, media data are delivered in multiple streams which is groups multiple packets.
