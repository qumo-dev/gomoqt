---
title: Session
weight: 3
---

A MOQ Session is established when a client dials a server (via `moqt.Dialer`) or a server accepts a connection (via `moqt.Handler`). The session manages subscriptions, announcements, fetches, and probes over the underlying QUIC connection.

## Implementation

### `moqt.Session`

```go
type Session struct {
    // contains filtered or unexported fields
}

func (s *Session) Subscribe(ctx context.Context, path BroadcastPath, name TrackName, config *SubscribeConfig) (*TrackReader, error)
func (s *Session) AcceptAnnounce(prefix string) (*AnnouncementReader, error)
func (s *Session) Fetch(req *FetchRequest) (*GroupReader, error)
func (s *Session) Probe(bitrate uint64) (*ProbeResult, error)
func (s *Session) CloseWithError(code SessionErrorCode, msg string) error
func (s *Session) Context() context.Context
func (s *Session) ConnectionState() ConnectionState
func (s *Session) LocalAddr() net.Addr
func (s *Session) RemoteAddr() net.Addr
```

Outgoing requests such as subscribing to tracks, fetching specific groups, probing bitrate, or discovering available tracks are handled by the session.

## Connection State

After a session is established, you can retrieve connection metadata via `ConnectionState()`:

```go
    state := sess.ConnectionState()
    fmt.Println("Protocol version:", state.Version) // e.g., "moq-lite-04"
    fmt.Println("TLS state:", state.TLS)
```

The `ConnectionState` struct contains:

| Field     | Type                      | Description                                 |
|-----------|---------------------------|---------------------------------------------|
| `Version` | `string`                  | The negotiated MOQ protocol version (e.g., `"moq-lite-04"`) |
| `TLS`     | `*tls.ConnectionState`     | TLS connection state when available          |

## Subscribe to a Track

{{<cards>}}
    {{< card link="../subscribe/#subscribe-to-a-track" title="Subscribe to a Track" icon="external-link">}}
{{</cards>}}

## Discover Available Broadcasts

{{<cards>}}
    {{< card link="../announce_discover/#discover-broadcasts" title="Discover Broadcasts" icon="external-link">}}
{{</cards>}}

## Fetch a Group

{{<cards>}}
    {{< card link="../fetch/" title="Fetch" icon="external-link">}}
{{</cards>}}

## Probe Bitrate

{{<cards>}}
    {{< card link="../probe/" title="Probe" icon="external-link">}}
{{</cards>}}

## Incoming Requests

Incoming requests, such as track subscriptions and discovery broadcasts, are handled internally by the session's `moqt.TrackMux`, not directly by the `moqt.Session` struct. Therefore, there are no dedicated methods for these requests on `moqt.Session`.

### Handle Track Subscriptions

{{<cards>}}
    {{< card link="../publish/#handle-track-subscriptions" title="Handle Track Subscriptions" icon="external-link">}}
{{</cards>}}

### Announce Broadcasts

{{<cards>}}
    {{< card link="../announce_discover/#announce-broadcasts" title="Announce Broadcasts" icon="external-link">}}
{{</cards>}}

## Terminating a Session

To explicitly close a session due to protocol violations, errors, or other reasons, use the `Session.CloseWithError` method. This closes all associated streams.

```go
func (s *Session) CloseWithError(code SessionErrorCode, msg string) error
```

- `code`: Error code (e.g., from built-in codes)
- `msg`: Descriptive message

Prefer reserved error codes for standard reasons. See [Built-in Error Codes](errors/#built-in-error-codes) for details.
