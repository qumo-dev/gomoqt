# Protocol Specification [Version: Development (0xfeedbabe)]

This implementation is based on **moq-lite-draft-05**.

Not yet implemented from draft-05:

- QUIC datagram delivery for groups (Section 6.4)
- Qmux transport bindings over TCP/TLS and WebSocket (Sections 4.2.1, 4.2.2)

## Extensions

These are additions to moq-lite-05. A peer that does not implement one is
never sent it.

### SUBSCRIBE_REDIRECT

A publisher may answer a SUBSCRIBE with SUBSCRIBE_REDIRECT instead of
SUBSCRIBE_OK, telling the subscriber where to subscribe instead. It is modelled
on moq-transport's REDIRECT error code and Redirect structure
(draft-ietf-moq-transport-22, sections 9.4.1 and 12.3), carried as a typed
message on the Subscribe Stream next to SUBSCRIBE_OK (0x0), SUBSCRIBE_END (0x1)
and SUBSCRIBE_DROP (0x2).

```
SUBSCRIBE_REDIRECT Message {
  Type (i) = 0x3
  Message Length (i)
  Connect URI (s)
  Broadcast Path (s)
  Track Name (s)
  Max Age (i)
}
```

- **Connect URI**: the session to subscribe on. Empty means the current
  session. A server that receives a non-empty Connect URI MUST close the
  session with PROTOCOL_VIOLATION: only a server may send a subscriber to
  another host.
- **Broadcast Path** and **Track Name**: the track to subscribe to instead.
- **Max Age**: how long, in milliseconds, the subscriber may reuse the redirect
  for later subscriptions to the same track without asking. 0 means not at all.

Rules:

- A publisher MUST NOT send SUBSCRIBE_REDIRECT unless the subscriber's SETUP
  carried the Redirect Setup Parameter (ID `0x7d01`, empty value), which
  advertises that it follows one. The ID is not assigned by moq-lite.
- SUBSCRIBE_REDIRECT is the only message the publisher sends on the stream; it
  then closes the stream (FIN). It MUST NOT follow SUBSCRIBE_OK or
  SUBSCRIBE_END.
- The subscriber re-subscribes on a new Subscribe Stream, with the same
  subscription parameters. A Subscribe Stream is bound to one track, so it is
  never re-targeted.
- A subscriber MUST bound how many redirects it follows in a row for one
  subscription, so publishers that redirect to each other cannot loop.
  gomoqt follows two by default.

## Reference

[Media over QUIC - Lite Draft 05](https://www.ietf.org/archive/id/draft-lcurley-moq-lite-05.html)
