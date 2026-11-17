# Protocol Specification [Version: Development (0xfeedbabe)]

This implementation is based on **moq-lite-draft-06**. The ALPN token (and
WebTransport protocol) is `moq-lite-06`; earlier drafts are not supported.

Not yet implemented from draft-06:

- QUIC datagram delivery for groups (Section 6.4)
- Qmux transport bindings over TCP/TLS and WebSocket (Section 4.2)
- The SETUP Role parameter (Section 7.3.3) is not sent, and a server ignores
  it; a client that receives one closes the session, as required.

Relay routing is wire-complete but not exposed for configuration: an endpoint
sends neutral defaults and leaves route selection to the relay.

- ANNOUNCE_START / ANNOUNCE_UPDATE carry Warm and Cold Route Cost. A local
  announcement advertises 0 for both; a received one is stored with the peer's
  SETUP Cost (default 1) added, saturating, and forwarded that way.
- The SETUP Cost parameter (0x4) is never sent (the default of 1 applies);
  the SETUP Hop parameter (0x5) is sent when the TrackMux has a Hop ID.
- Receivers apply the local checks: a repeated non-zero Hop ID in one
  announcement closes the session with PROTOCOL_VIOLATION, an announcement
  whose path contains the receiver's own Hop ID is discarded, and a publisher
  never advertises (nor serves a subscription from) a path containing the Hop
  ID the subscriber declared in SETUP.
- Route selection, cost ranking and splicing across routes are left to the
  relay.
- A TrackMux advertises each published path as a route and presents a route
  above a requested prefix clamped to it. It serves a subscription from the
  most specific route covering its path: the path itself if published, else
  the longest published route above it, matched per segment. A path no route
  covers is refused with NOT_FOUND. A route received from a peer whose Auth
  grant does not cover every path beneath it serves only its own path.

Frame positions (Section 3.6) are handled internally, with no public API.
gomoqt always requests whole groups: it sends Frame Start and Frame End as 0
on SUBSCRIBE, SUBSCRIBE_UPDATE and FETCH. It serves a peer's frame-bounded
SUBSCRIBE or FETCH correctly: the start and end groups go out partially, with
GROUP's Frame Start set, and a range that breaks Section 3.6 or 7.16 is reset.

Stream error codes gomoqt needs and moq-lite does not define are assigned in
the application range: 0x40 unauthorized, 0x41 protocol violation (a range or
frame rule broken on a stream), 0x42 duplicate Subscribe ID, 0x43 invalid
prefix, 0x44 banned prefix, 0x45 not supported (a Probe Stream at Probe level
None, an unknown stream type). The Auth Stream's NOT_SUPPORTED session code is
0x40 in the session space.

## Extensions

These are additions to moq-lite-06. A peer that does not implement one is
never sent it.

### SUBSCRIBE_REDIRECT

A publisher may answer a SUBSCRIBE with SUBSCRIBE_REDIRECT instead of
SUBSCRIBE_OK, telling the subscriber where to subscribe instead. It is modelled
on moq-transport's REDIRECT error code and REDIRECT Error Detail
(draft-ietf-moq-transport-23, sections 9.4.2.1 and 12.3), carried as a typed
message on the Subscribe Stream next to SUBSCRIBE_OK (0x0), SUBSCRIBE_END (0x1)
and SUBSCRIBE_DROP (0x2).

```
SUBSCRIBE_REDIRECT Message {
  Type (i) = 0x3
  Message Length (i)
  Connect URI (s)
  Broadcast Path (s)
  Track Name (s)
}
```

- **Connect URI**: the session to subscribe on. Empty means the current
  session. A server that receives a non-empty Connect URI MUST close the
  session with PROTOCOL_VIOLATION: only a server may send a subscriber to
  another host. (moq-transport-23 uses ROLE_VIOLATION here; moq-lite has no
  such code, so PROTOCOL_VIOLATION is kept.)
- **Broadcast Path** and **Track Name**: the track to subscribe to instead.

Rules:

- A publisher MUST NOT send SUBSCRIBE_REDIRECT unless the subscriber's SETUP
  carried the Redirect Setup Parameter (ID `0x7d01`), which advertises that it
  follows one. The ID is not assigned by moq-lite. The parameter's value is
  the newest version of SUBSCRIBE_REDIRECT the sender understands, a varint,
  and it understands every earlier one; empty means version 0, the format
  above. A malformed value means the sender does not follow redirects. A
  later version, for example one that adds a field, is sent only to a peer
  that advertised it.
- SUBSCRIBE_REDIRECT is the only message the publisher sends on the stream; it
  then closes the stream (FIN). It MUST NOT follow SUBSCRIBE_OK or
  SUBSCRIBE_END; a subscriber that receives one there closes the session with
  PROTOCOL_VIOLATION.
- The subscriber re-subscribes on a new Subscribe Stream, with the same
  subscription parameters. A Subscribe Stream is bound to one track, so it is
  never re-targeted.
- A subscriber MUST bound how many redirects it follows in a row for one
  subscription, so publishers that redirect to each other cannot loop.
  gomoqt follows two by default and at most ten.

## Reference

[Media over QUIC - Lite Draft 06](https://www.ietf.org/archive/id/draft-lcurley-moq-lite-06.html)
